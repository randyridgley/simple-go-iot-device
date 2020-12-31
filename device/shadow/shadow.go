/*
Copyright © 2020 Randy Ridgley randy.ridgley@gmail.com

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package shadow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/randyridgley/simple-go-iot-device/device"
)

// Shadow is an interface of Thing Shadow.
type Shadow interface {
	// Get thing state and update local state document.
	Get(ctx context.Context) (*ThingDocument, error)
	// Report thing state and update local state document.
	Report(ctx context.Context, state interface{}) (*ThingDocument, error)
	// Desire sets desired thing state and update local state document.
	Desire(ctx context.Context, state interface{}) (*ThingDocument, error)
	// Document returns full thing document.
	Document() *ThingDocument
	// Delete thing shadow.
	Delete(ctx context.Context) error
	// OnDelta sets handler of state deltas.
	OnDelta(func(delta map[string]interface{}))
	// OnError sets handler of asynchronous errors.
	OnError(func(error))
}

var ErrRejected = errors.New("rejected")

// ErrInvalidResponse is returned if failed to parse response from AWS IoT.
var ErrInvalidResponse = errors.New("invalid response from AWS IoT")

type shadow struct {
	thing     device.Thing
	thingName string
	doc       *ThingDocument
	onDelta   func(delta map[string]interface{})
	onError   func(err error)
	mu        sync.Mutex
	chResps   map[string]chan interface{}
	msgToken  uint32
}

func (s *shadow) token() string {
	token := atomic.AddUint32(&s.msgToken, 1)
	return fmt.Sprintf("%x", token)
}

func (s *shadow) topic(operation string) string {
	return "$aws/things/" + s.thingName + "/shadow/" + operation
}

func New(ctx context.Context, thing device.Thing) (Shadow, error) {
	s := &shadow{
		thing:     thing,
		thingName: thing.Config.ThingName,
		doc: &ThingDocument{
			State: ThingState{
				Desired:  map[string]interface{}{},
				Reported: map[string]interface{}{},
				Delta:    map[string]interface{}{},
			},
		},

		chResps: make(map[string]chan interface{}),
	}

	for _, sub := range []struct {
		topic   string
		handler mqtt.MessageHandler
	}{
		{s.topic("update/delta"), mqtt.MessageHandler(s.updateDelta)},
		{s.topic("update/accepted"), mqtt.MessageHandler(s.updateAccepted)},
		{s.topic("update/rejected"), mqtt.MessageHandler(s.rejected)},
		{s.topic("delete/accepted"), mqtt.MessageHandler(s.deleteAccepted)},
		{s.topic("delete/rejected"), mqtt.MessageHandler(s.rejected)},
		{s.topic("get/accepted"), mqtt.MessageHandler(s.getAccepted)},
		{s.topic("get/rejected"), mqtt.MessageHandler(s.rejected)},
	} {
		if err := thing.Connection.Subscribe(sub.topic, sub.handler); err != nil {
			return nil, fmt.Errorf("registering message handlers %v", err)
		}
	}

	return s, nil
}

func (s *shadow) handleResponse(r interface{}) {
	fmt.Println("received response")
	token, ok := clientToken(r)
	if !ok {
		return
	}
	s.mu.Lock()
	ch, ok := s.chResps[token]
	s.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- r:
	default:
	}
}

func (s *shadow) getAccepted(client mqtt.Client, msg mqtt.Message) {
	doc := &ThingDocument{}
	if err := json.Unmarshal(msg.Payload(), doc); err != nil {
		s.handleError(fmt.Errorf("unmarshaling thing document  %v", err))
		return
	}
	s.mu.Lock()
	s.doc = doc
	s.mu.Unlock()
	s.handleResponse(doc)

	s.handleDelta(doc.State.Delta)
}

func (s *shadow) rejected(client mqtt.Client, msg mqtt.Message) {
	e := &ErrorResponse{}
	if err := json.Unmarshal(msg.Payload(), e); err != nil {
		s.handleError(fmt.Errorf("unmarshaling error response %v", err))
		return
	}
	s.handleResponse(e)
}

func (s *shadow) updateAccepted(client mqtt.Client, msg mqtt.Message) {
	doc := &thingDocumentRaw{}
	if err := json.Unmarshal(msg.Payload(), doc); err != nil {
		s.handleError(fmt.Errorf("unmarshaling thing document %v", err))
		return
	}
	s.mu.Lock()
	err := s.doc.update(doc)
	s.mu.Unlock()
	if err != nil {
		s.handleError(fmt.Errorf("updating local thing document %v", err))
		return
	}
	s.handleResponse(doc)
}

func (s *shadow) updateDelta(client mqtt.Client, msg mqtt.Message) {
	state := &thingDelta{}
	if err := json.Unmarshal(msg.Payload(), state); err != nil {
		s.handleError(fmt.Errorf("unmarshaling thing delta %v", err))
		return
	}
	s.mu.Lock()
	ok := s.doc.updateDelta(state)
	delta := cloneState(s.doc.State.Delta)
	s.mu.Unlock()
	if ok {
		s.handleDelta(delta)
	}
}

func (s *shadow) deleteAccepted(client mqtt.Client, msg mqtt.Message) {
	doc := &thingDocumentRaw{}
	if err := json.Unmarshal(msg.Payload(), doc); err != nil {
		s.handleError(fmt.Errorf("unmarshaling thing document %v", err))
		return
	}
	s.mu.Lock()
	s.doc = nil
	s.mu.Unlock()
	s.handleResponse(doc)
}

func (s *shadow) Report(ctx context.Context, state interface{}) (*ThingDocument, error) {
	rawState, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("marshaling state %v", err)
	}
	token := s.token()
	rawStateJSON := json.RawMessage(rawState)
	data, err := json.Marshal(&thingDocumentRaw{
		State:       thingStateRaw{Reported: rawStateJSON},
		ClientToken: token,
	})
	if err != nil {
		return nil, fmt.Errorf("marshaling request %v", err)
	}

	ch := make(chan interface{}, 1)
	s.mu.Lock()
	s.chResps[token] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.chResps, token)
		s.mu.Unlock()
	}()

	if token := s.thing.Connection.Publish(s.topic("update"), data); token.Wait() && token.Error() != nil {
		return nil, fmt.Errorf("sending request %v", err)
	}

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("updating reported state %v", ctx.Err())
	case res := <-ch:
		switch r := res.(type) {
		case *thingDocumentRaw:
			s.mu.Lock()
			doc := s.doc.clone()
			s.mu.Unlock()
			return doc, nil
		case *ErrorResponse:
			return nil, r
		default:
			return nil, fmt.Errorf("updating reported state %v", ErrInvalidResponse)
		}
	}
}

func (s *shadow) Desire(ctx context.Context, state interface{}) (*ThingDocument, error) {
	rawState, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("marshaling state %v", err)
	}
	token := s.token()
	rawStateJSON := json.RawMessage(rawState)
	data, err := json.Marshal(&thingDocumentRaw{
		State:       thingStateRaw{Desired: rawStateJSON},
		ClientToken: token,
	})
	if err != nil {
		return nil, fmt.Errorf("marshaling request %v", err)
	}

	ch := make(chan interface{}, 1)
	s.mu.Lock()
	s.chResps[token] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.chResps, token)
		s.mu.Unlock()
	}()

	if token := s.thing.Connection.Publish(s.topic("update"), data); token.Wait() && token.Error() != nil {
		return nil, fmt.Errorf("sending request %v", err)
	}

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("updating reported state %v", ctx.Err())
	case res := <-ch:
		switch r := res.(type) {
		case *thingDocumentRaw:
			s.mu.Lock()
			doc := s.doc.clone()
			s.mu.Unlock()
			return doc, nil
		case *ErrorResponse:
			return nil, r
		default:
			return nil, fmt.Errorf("updating desired state %v", ErrInvalidResponse)
		}
	}
}

func (s *shadow) Get(ctx context.Context) (*ThingDocument, error) {
	token := s.token()
	data, err := json.Marshal(&simpleRequest{
		ClientToken: token,
	})
	if err != nil {
		return nil, fmt.Errorf("marshaling request %v", err)
	}

	ch := make(chan interface{}, 1)
	s.mu.Lock()
	s.chResps[token] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.chResps, token)
		s.mu.Unlock()
	}()

	if token := s.thing.Connection.Publish(s.topic("get"), []byte(data)); token.Wait() && token.Error() != nil {
		return nil, fmt.Errorf("sending request %v", err)
	}

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("updating reported state %v", ctx.Err())
	case res := <-ch:
		switch r := res.(type) {
		case *ThingDocument:
			setClientToken(r, "")
			return r, nil
		case *ErrorResponse:
			return nil, r
		default:
			return nil, fmt.Errorf("getting document %v", ErrInvalidResponse)
		}
	}
}

func (s *shadow) Delete(ctx context.Context) error {
	token := s.token()
	data, err := json.Marshal(&simpleRequest{
		ClientToken: token,
	})
	if err != nil {
		return fmt.Errorf("marshaling request %v", err)
	}

	ch := make(chan interface{}, 1)
	s.mu.Lock()
	s.chResps[token] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.chResps, token)
		s.mu.Unlock()
	}()

	if token := s.thing.Connection.Publish(s.topic("delete"), []byte(data)); token.Wait() && token.Error() != nil {
		return fmt.Errorf("sending request %v", err)
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("updating reported state %v", ctx.Err())
	case res := <-ch:
		switch r := res.(type) {
		case *thingDocumentRaw:
			return nil
		case *ErrorResponse:
			return r
		default:
			return fmt.Errorf("deleting state %v", ErrInvalidResponse)
		}
	}
}

func (s *shadow) Document() *ThingDocument {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.doc.clone()
}

func (s *shadow) OnDelta(cb func(delta map[string]interface{})) {
	s.mu.Lock()
	s.onDelta = cb
	s.mu.Unlock()
}

func (s *shadow) handleDelta(delta map[string]interface{}) {
	s.mu.Lock()
	cb := s.onDelta
	s.mu.Unlock()
	if cb != nil {
		cb(delta)
	}
}

func (s *shadow) OnError(cb func(err error)) {
	s.mu.Lock()
	s.onError = cb
	s.mu.Unlock()
}

func (s *shadow) handleError(err error) {
	s.mu.Lock()
	cb := s.onError
	s.mu.Unlock()
	if cb != nil {
		cb(err)
	}
}

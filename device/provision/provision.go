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
package provision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/randyridgley/simple-go-iot-device/device"
	"github.com/randyridgley/simple-go-iot-device/device/connect"
	"github.com/spf13/viper"
)

// Provision is an interface of Thing Provisioning.
type Provision interface {
	Provision(ctx context.Context, provisionChan chan bool)
	Disconnect(ctx context.Context)
}

var ErrRejected = errors.New("rejected")

// ErrInvalidResponse is returned if failed to parse response from AWS IoT.
var ErrInvalidResponse = errors.New("invalid response from AWS IoT")

type RegisterThingRequest struct {
	CertificateOwnershipToken string     `json:"certificateOwnershipToken"`
	Parameters                Parameters `json:"parameters"`
}

type Parameters struct {
	SerialNumber   string `json:"serialNumber"`
	DeviceLocation string `json:"deviceLocation"`
}

func NewRegisterThingRequest(p *provision) *RegisterThingRequest {
	return &RegisterThingRequest{
		CertificateOwnershipToken: p.KeysAndCertificateResponse.CertificateOwnershipToken,
		Parameters: Parameters{
			DeviceLocation: p.thing.Config.DeviceLocation,
			SerialNumber:   p.thing.Config.SerialNumber,
		},
	}
}

type provision struct {
	thing     device.Thing
	thingName string
	// doc       *ThingDocument
	// onDelta   func(delta map[string]interface{})
	// onError   func(err error)
	mu                         sync.Mutex
	Channels                   Channels
	KeysAndCertificateResponse struct {
		CertificateId             string `json:"certificateId"`
		CertificatePem            string `json:"certificatePem"`
		PrivateKey                string `json:"privateKey"`
		CertificateOwnershipToken string `json:"certificateOwnershipToken"`
	}
	RegisterThingResponse struct {
		ThingName           string `json:"thingName"`
		DeviceConfiguration string `json:"deviceConfiguration"`
	}
	Connection connect.Connection
	// msgToken  uint32
}

type Channels struct {
	RegisterKeysChan  chan bool
	RegisterThingChan chan bool
	ProvisionChan     chan bool
}

func New(tx context.Context, thing device.Thing, bootstrapKeypair connect.KeyPair) (Provision, error) {
	conf := connect.ConnectionConfiguration{
		KeyPair:  bootstrapKeypair,
		Endpoint: thing.Config.Endpoint,
		Port:     thing.Config.Port,
		ClientId: thing.Config.ThingName,
	}
	c, err := connect.New(&conf)
	if err != nil {
		return nil, fmt.Errorf("Could not create connection %v", err)
	}
	c.Connect()
	p := &provision{
		thing:      thing,
		thingName:  thing.Config.ThingName,
		Connection: c,
		Channels: Channels{
			RegisterKeysChan:  make(chan bool, 10),
			RegisterThingChan: make(chan bool, 10),
		},
	}
	fmt.Printf("Bootstrap Connected to %s\n", thing.Config.Endpoint)

	for _, sub := range []struct {
		topic   string
		handler mqtt.MessageHandler
	}{
		{"$aws/certificates/create/json/accepted", mqtt.MessageHandler(p.certificateCreateAccepted)},
		{"$aws/certificates/create/json/rejected", mqtt.MessageHandler(p.certificateCreateRejected)},
		{fmt.Sprintf("$aws/provisioning-templates/%s/provision/json/accepted", p.thing.Config.ProvisioningTemplate), mqtt.MessageHandler(p.provisioningAccepted)},
		{fmt.Sprintf("$aws/provisioning-templates/%s/provision/json/rejected", p.thing.Config.ProvisioningTemplate), mqtt.MessageHandler(p.provisioningRejected)},
	} {
		if err := p.Connection.Subscribe(sub.topic, sub.handler); err != nil {
			return nil, fmt.Errorf("registering message handlers %v", err)
		}
	}

	return p, nil
}

func (p *provision) certificateCreateAccepted(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("* [%s] %s\n", msg.Topic(), string(msg.Payload()))
	json.Unmarshal(msg.Payload(), &p.KeysAndCertificateResponse)
	p.Channels.RegisterKeysChan <- true
}

func (p *provision) certificateCreateRejected(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("* [%s] %s\n", msg.Topic(), string(msg.Payload()))
	p.Channels.RegisterKeysChan <- false
}

func (p *provision) provisioningAccepted(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("* [%s] %s\n", msg.Topic(), string(msg.Payload()))
	json.Unmarshal(msg.Payload(), &p.RegisterThingResponse)
	certFileName := fmt.Sprintf("certs/%s.certificate.pem", p.thing.Config.ThingName)
	keyFileName := fmt.Sprintf("certs/%s.private.key", p.thing.Config.ThingName)

	if &p.RegisterThingResponse != nil {
		// save the keys if need to otherwise start connecting and sending data
		f, err := os.Create(fmt.Sprintf("%s", certFileName))

		if err != nil {
			log.Fatal(err)
		}

		defer f.Close()

		_, err2 := f.WriteString(p.KeysAndCertificateResponse.CertificatePem)

		if err2 != nil {
			log.Fatal(err2)
		}
		viper.Set("primary.certificatepath", fmt.Sprintf("%s", certFileName))

		f, err = os.Create(fmt.Sprintf("%s", keyFileName))

		if err != nil {
			log.Fatal(err)
		}

		defer f.Close()

		_, err2 = f.WriteString(p.KeysAndCertificateResponse.PrivateKey)

		if err2 != nil {
			log.Fatal(err2)
		}
		viper.Set("primary.privatekeypath", fmt.Sprintf("%s", keyFileName))
		viper.WriteConfig()
		fmt.Printf("wrote files: cert_file_name: %v key_file_name: %v\n", certFileName, keyFileName)
	}
	p.Channels.ProvisionChan <- true
}

func (p *provision) provisioningRejected(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("* [%s] %s\n", msg.Topic(), string(msg.Payload()))
	p.Channels.ProvisionChan <- false
}

func (p *provision) Provision(ctx context.Context, provisionChan chan bool) {
	fmt.Println("Creating keys and certificates in AWS IoT")
	p.Channels.ProvisionChan = provisionChan
	p.Connection.Publish("$aws/certificates/create/json", []byte{})
	go p.RegisterThing(ctx)
}

func (p *provision) Disconnect(ctx context.Context) {
	p.Connection.Disconnect(1)
}

func (p *provision) PublishThingRequest() {
	payload, _ := json.Marshal(NewRegisterThingRequest(p))
	s := string(payload)
	fmt.Println(s)
	token := p.Connection.Publish(fmt.Sprintf("$aws/provisioning-templates/%s/provision/json", p.thing.Config.ProvisioningTemplate), payload)
	token.Wait() //fix
}

func (p *provision) RegisterThing(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("updating reported state %v", ctx.Err())
		case accepted := <-p.Channels.RegisterKeysChan:
			if accepted {
				fmt.Println("Creating thing in AWS IoT")
				p.PublishThingRequest()
			} else {
				fmt.Println("Create Keys and Certificates rejected")
				p.Channels.ProvisionChan <- false
			}
		}
	}
}

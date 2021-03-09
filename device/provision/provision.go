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
	"sync/atomic"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/randyridgley/simple-go-iot-device/device"
	"github.com/randyridgley/simple-go-iot-device/device/connect"
	"github.com/spf13/viper"
)

const certAccepted = "$aws/certificates/create/json/accepted"
const certRejected = "$aws/certificates/create/json/rejected"
const certCreate = "$aws/certificates/create/json"

// Provision is an interface of Thing Provisioning.
type Provision interface {
	Provision(ctx context.Context)
	Disconnect(ctx context.Context)
}

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

func NewRegisterThingRequest(p *Provisioner) *RegisterThingRequest {
	return &RegisterThingRequest{
		CertificateOwnershipToken: p.KeysAndCertificateResponse.CertificateOwnershipToken,
		Parameters: Parameters{
			DeviceLocation: p.thing.Config.DeviceLocation,
			SerialNumber:   p.thing.Config.SerialNumber,
		},
	}
}

func (p *Provisioner) token() string {
	token := atomic.AddUint32(&p.msgToken, 1)
	return fmt.Sprintf("%x", token)
}

type Provisioner struct {
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
	chResps    map[string]chan interface{}
	msgToken   uint32
}

type Channels struct {
	RegisterKeysChan  chan bool
	RegisterThingChan chan bool
	ProvisionChan     chan bool
}

// New - Function to create a new Provisioner
func New(tx context.Context, thing device.Thing, bootstrapKeypair connect.KeyPair) (*Provisioner, error) {
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
	p := &Provisioner{
		thing:      thing,
		thingName:  thing.Config.ThingName,
		Connection: c,
		Channels: Channels{
			RegisterKeysChan:  make(chan bool, 1),
			RegisterThingChan: make(chan bool, 1),
		},
	}
	fmt.Printf("Bootstrap Connected to %s\n", thing.Config.Endpoint)

	for _, sub := range []struct {
		topic   string
		handler mqtt.MessageHandler
	}{
		{certAccepted, mqtt.MessageHandler(p.certificateCreateAccepted)},
		{certRejected, mqtt.MessageHandler(p.certificateCreateRejected)},
		{fmt.Sprintf("$aws/provisioning-templates/%s/provision/json/accepted", p.thing.Config.ProvisioningTemplate), mqtt.MessageHandler(p.provisioningAccepted)},
		{fmt.Sprintf("$aws/provisioning-templates/%s/provision/json/rejected", p.thing.Config.ProvisioningTemplate), mqtt.MessageHandler(p.provisioningRejected)},
	} {
		if err := p.Connection.Subscribe(sub.topic, sub.handler); err != nil {
			return nil, fmt.Errorf("registering message handlers %v", err)
		}
	}

	return p, nil
}

func (p *Provisioner) certificateCreateAccepted(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("* [%s] %s\n", msg.Topic(), string(msg.Payload()))
	json.Unmarshal(msg.Payload(), &p.KeysAndCertificateResponse)
	p.Channels.RegisterKeysChan <- true
}

func (p *Provisioner) certificateCreateRejected(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("* [%s] %s\n", msg.Topic(), string(msg.Payload()))
	p.Channels.RegisterKeysChan <- false
}

func (p *Provisioner) provisioningAccepted(client mqtt.Client, msg mqtt.Message) {
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
	fmt.Printf("Writing to provision complete channel")
	p.Channels.RegisterThingChan <- true
}

func (p *Provisioner) provisioningRejected(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("* [%s] %s\n", msg.Topic(), string(msg.Payload()))
	p.Channels.RegisterThingChan <- false
}

func (p *Provisioner) Provision(ctx context.Context) {
	go func() {
		fmt.Println("Creating keys and certificates in AWS IoT")
		if token := p.Connection.Publish(certCreate, []byte{}); token.Error() != nil {
			fmt.Errorf("registering message handlers %v", token.Error())
		}
	}()
	fmt.Println("Waiting for certificate provisioning.")
	go p.RegisterThing(ctx)

	for {
		select {
		case <-ctx.Done():
			fmt.Errorf("updating reported state %v", ctx.Err())
		case accepted := <-p.Channels.RegisterThingChan:
			if accepted {
				fmt.Println("Thing provisioning completed.")
				p.Disconnect(ctx)
			} else {
				fmt.Println("Thing provisioning failed.")
			}
			return
		}
	}
}

func (p *Provisioner) Disconnect(ctx context.Context) {
	p.Connection.Disconnect(3)
}

func (p *Provisioner) PublishThingRequest() {
	payload, _ := json.Marshal(NewRegisterThingRequest(p))
	topic := fmt.Sprintf("$aws/provisioning-templates/%s/provision/json", p.thing.Config.ProvisioningTemplate)
	fmt.Printf("* [%s] %s\n", topic, string(payload))
	if token := p.Connection.Publish(topic, payload); token.Error() != nil {
		fmt.Errorf("publish thing request %v", token.Error())
	}
}

func (p *Provisioner) RegisterThing(ctx context.Context) {
registerThing:
	for {
		select {
		case <-ctx.Done():
			fmt.Errorf("updating reported state %v", ctx.Err())
			break registerThing
		case accepted := <-p.Channels.RegisterKeysChan:
			if accepted {
				fmt.Println("Creating thing in AWS IoT")
				p.PublishThingRequest()
			} else {
				fmt.Println("Create Keys and Certificates rejected")
				p.Channels.ProvisionChan <- false
			}
			fmt.Println("Returned from register keys channel")
			break registerThing
		}
	}
	fmt.Println("returning from register thing")
}

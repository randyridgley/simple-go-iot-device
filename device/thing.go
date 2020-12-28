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
package device

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/randyridgley/simple-go-iot-device/config"
)

type Thing struct {
	Client                     mqtt.Client
	ThingName                  string
	KeysAndCertificateResponse struct {
		CertificateId             string `json:"certificateId"`
		CertificatePem            string `json:"certificatePem"`
		PrivateKey                string `json:"privateKey"`
		CertificateOwnershipToken string `json:"certificateOwnershipToken"`
	}
	registerKeysChan     chan bool
	registerThingChan    chan bool
	provisioningTemplate string
	serialNumber         string
	deviceLocation       string
}

type KeyPair struct {
	PrivateKeyPath    string
	CertificatePath   string
	CACertificatePath string
}

type RegisterThingRequest struct {
	CertificateOwnershipToken string     `json:"certificateOwnershipToken"`
	Parameters                Parameters `json:"parameters"`
}

type Parameters struct {
	SerialNumber   string `json:"serialNumber"`
	DeviceLocation string `json:"deviceLocation"`
}

func NewThing(keyPair KeyPair, thingName string, config config.Configurations) (*Thing, error) {
	tlsCert, err := tls.LoadX509KeyPair(keyPair.CertificatePath, keyPair.PrivateKeyPath)

	if err != nil {
		return nil, fmt.Errorf("failed to load certs: %v", err)
	}

	certs := x509.NewCertPool()

	caPEM, err := ioutil.ReadFile(keyPair.CACertificatePath)

	if err != nil {
		return nil, err
	}

	certs.AppendCertsFromPEM(caPEM)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		RootCAs:      certs,
	}

	if err != nil {
		return nil, err
	}

	serverURL := fmt.Sprintf("tcps://%s:8883", config.Server.Endpoint)
	fmt.Printf("Connecting to %s\n", serverURL)

	mqttOpts := mqtt.NewClientOptions()
	mqttOpts.AddBroker(serverURL)
	mqttOpts.CleanSession = false
	mqttOpts.AutoReconnect = true
	mqttOpts.SetMaxReconnectInterval(1 * time.Second)
	mqttOpts.SetClientID(thingName)
	mqttOpts.SetTLSConfig(tlsConfig)

	c := mqtt.NewClient(mqttOpts)

	if token := c.Connect(); token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}

	return &Thing{
		Client:               c,
		ThingName:            thingName,
		registerKeysChan:     make(chan bool),
		registerThingChan:    make(chan bool),
		provisioningTemplate: config.Bootstrap.ProvisioningTemplate,
		deviceLocation:       config.DeviceLocation,
		serialNumber:         config.SerialNumber,
	}, nil
}

func (t *Thing) CreateKeysAndCertificate() {
	fmt.Println("Creating keys and certificates in AWS IoT")
	t.SubscribeToCreateKeysAndCertificateAccepted()
	t.SubscribeToCreateKeysAndCertificateRejected()

	t.PublishCreateKeysAndCertificate()

	for {
		select {
		case result := <-t.registerKeysChan:
			if result {
				t.registerThingChan <- true
			} else {
				fmt.Printf("Failed response %v", t.KeysAndCertificateResponse)
			}
		}
	}
}

func (t *Thing) CreateThing() {
	for {
		select {
		case accepted := <-t.registerThingChan:
			if accepted {
				fmt.Println("Creating thing in AWS IoT")

				t.SubscribeToRegisterThingAccepted()
				t.SubscribeToRegisterThingRejected()

				t.PublishThingRequest()
			} else {
				fmt.Println("Create Keys and Certificates rejected")
			}
		}
	}
}

func (t *Thing) Start() {
	fmt.Println("Starting thing...")
}

func (t *Thing) SubscribeToCreateKeysAndCertificateAccepted() {
	t.Client.Subscribe("$aws/certificates/create/json/accepted", 1, func(client mqtt.Client, msg mqtt.Message) {
		fmt.Printf("Accepted")
		fmt.Printf("* [%s] %s\n", msg.Topic(), string(msg.Payload()))
		json.Unmarshal(msg.Payload(), &t.KeysAndCertificateResponse)
		t.registerKeysChan <- true
	})
}

func (t *Thing) SubscribeToCreateKeysAndCertificateRejected() {
	t.Client.Subscribe("$aws/certificates/create/json/rejected", 1, func(client mqtt.Client, msg mqtt.Message) {
		fmt.Printf("Rejected")
		fmt.Printf("* [%s] %s\n", msg.Topic(), string(msg.Payload()))
		t.registerKeysChan <- false
	})
}

func (t *Thing) SubscribeToRegisterThingAccepted() {
	path := fmt.Sprintf("$aws/provisioning-templates/%s/provision/json/accepted", t.provisioningTemplate)
	fmt.Printf("Subscribing to %s\n", path)
	t.Client.Subscribe(path, 1, func(client mqtt.Client, msg mqtt.Message) {
		fmt.Printf("* [%s] %s\n", msg.Topic(), string(msg.Payload()))
	})
}

func (t *Thing) SubscribeToRegisterThingRejected() {
	path := fmt.Sprintf("$aws/provisioning-templates/%s/provision/json/rejected", t.provisioningTemplate)
	fmt.Printf("Subscribing to %s\n", path)

	t.Client.Subscribe(path, 1, func(client mqtt.Client, msg mqtt.Message) {
		fmt.Printf("* [%s] %s\n", msg.Topic(), string(msg.Payload()))
	})
}

func (t *Thing) PublishCreateKeysAndCertificate() {
	t.Client.Publish("$aws/certificates/create/json", 0, false, []byte{})
}

func (t *Thing) PublishThingRequest() {
	payload, _ := json.Marshal(NewRegisterThingRequest(t))
	s := string(payload)
	fmt.Println(s)
	t.Client.Publish(fmt.Sprintf("$aws/provisioning-templates/%s/provision/json", t.provisioningTemplate), 1, false, payload)
}

// func (t *Thing) WaitForCreateKeysAndCertificateResponse() {
// 	for {
// 		select {}
// 	}
// }

func NewRegisterThingRequest(t *Thing) *RegisterThingRequest {
	request := &RegisterThingRequest{
		CertificateOwnershipToken: t.KeysAndCertificateResponse.CertificateOwnershipToken,
	}
	request.Parameters.DeviceLocation = t.deviceLocation
	request.Parameters.SerialNumber = t.serialNumber
	return request
}

// func (t *Thing) OnConnect() mqtt.OnConnectHandler {
// 	return func(client mqtt.Client) {
// 		log.Println("Running MQTT.OnConnectHandler")
// 		t.isConnected = true
// 	}
// }

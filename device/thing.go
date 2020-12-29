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
	"log"
	"os"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/spf13/viper"
)

type Thing struct {
	Client                     mqtt.Client
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
	registerKeysChan  chan bool
	registerThingChan chan bool
	startChan         chan string
	config            ThingConfiguration
	isConnected       bool
}

type ThingConfiguration struct {
	ThingName            string
	KeyPair              KeyPair
	DeviceLocation       string
	SerialNumber         string
	ProvisioningTemplate string
	Endpoint             string
	Port                 int
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

func NewThing(config ThingConfiguration) (*Thing, error) {
	c, _ := createMQTTClient(&config)

	return &Thing{
		Client:            c,
		registerKeysChan:  make(chan bool),
		registerThingChan: make(chan bool),
		startChan:         make(chan string, 5),
		config:            config,
	}, nil
}

func (t *Thing) Connect() (*Thing, error) {
	// connect to MQTT endpoint
	if token := t.Client.Connect(); token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}

	// Check if personal certs exist for device make this into a call to check both certs
	if _, err := os.Stat(fmt.Sprintf("certs/%s.certificate.pem", t.config.ThingName)); err == nil {
		go t.Start()
		t.startChan <- "READY"
	} else {
		go t.CreateKeysAndCertificate()
		go t.RegisterThing()
		go t.Start()
	}

	return t, nil
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

func (t *Thing) RegisterThing() {
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
	for {
		select {
		case result := <-t.startChan:
			certFileName := fmt.Sprintf("certs/%s.certificate.pem", t.config.ThingName)
			keyFileName := fmt.Sprintf("certs/%s.private.key", t.config.ThingName)

			switch {
			case result == "ACCEPTED":
				fmt.Println("Accepted thing...")

				if &t.RegisterThingResponse != nil {
					// save the keys if need to otherwise start connecting and sending data
					f, err := os.Create(fmt.Sprintf("%s", certFileName))

					if err != nil {
						log.Fatal(err)
					}

					defer f.Close()

					_, err2 := f.WriteString(t.KeysAndCertificateResponse.CertificatePem)

					if err2 != nil {
						log.Fatal(err2)
					}
					viper.Set("primary.certificatepath", fmt.Sprintf("%s", certFileName))

					f, err = os.Create(fmt.Sprintf("%s", keyFileName))

					if err != nil {
						log.Fatal(err)
					}

					defer f.Close()

					_, err2 = f.WriteString(t.KeysAndCertificateResponse.PrivateKey)

					if err2 != nil {
						log.Fatal(err2)
					}
					viper.Set("primary.privatekeypath", fmt.Sprintf("%s", keyFileName))
					viper.WriteConfig()
					fmt.Printf("wrote files: cert_file_name: %v key_file_name: %v\n", certFileName, keyFileName)
				}
				t.Client.Disconnect(250)
				t.startChan <- "READY"
				fmt.Println("Moving to ready state")
			case result == "READY":
				fmt.Println("Starting device.")
				t.config.KeyPair.CertificatePath = certFileName
				t.config.KeyPair.PrivateKeyPath = keyFileName
				c, _ := createMQTTClient(&t.config)
				t.Client = c
				// connect to MQTT endpoint
				if token := t.Client.Connect(); token.Wait() && token.Error() != nil {
					fmt.Printf("%v", token.Error())
				}

				payload := "{\"let-me\": \"in\"}"
				t.Client.Publish("fleet/2974685", 1, false, payload)
				fmt.Println("Published message")
			case result == "REJECTED":
				fmt.Printf("Failed response %v", t.RegisterThingResponse)

			}
		}
	}
}

//func to check for existance of pem and key

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
	path := fmt.Sprintf("$aws/provisioning-templates/%s/provision/json/accepted", t.config.ProvisioningTemplate)
	fmt.Printf("Subscribing to %s\n", path)
	t.Client.Subscribe(path, 1, func(client mqtt.Client, msg mqtt.Message) {
		fmt.Printf("* [%s] %s\n", msg.Topic(), string(msg.Payload()))
		json.Unmarshal(msg.Payload(), &t.RegisterThingResponse)
		t.startChan <- "ACCEPTED"
	})
}

func (t *Thing) SubscribeToRegisterThingRejected() {
	path := fmt.Sprintf("$aws/provisioning-templates/%s/provision/json/rejected", t.config.ProvisioningTemplate)
	fmt.Printf("Subscribing to %s\n", path)

	t.Client.Subscribe(path, 1, func(client mqtt.Client, msg mqtt.Message) {
		fmt.Printf("* [%s] %s\n", msg.Topic(), string(msg.Payload()))
		t.startChan <- "REJECTED"
	})
}

func (t *Thing) PublishCreateKeysAndCertificate() {
	t.Client.Publish("$aws/certificates/create/json", 0, false, []byte{})
}

func (t *Thing) PublishThingRequest() {
	payload, _ := json.Marshal(NewRegisterThingRequest(t))
	s := string(payload)
	fmt.Println(s)
	t.Client.Publish(fmt.Sprintf("$aws/provisioning-templates/%s/provision/json", t.config.ProvisioningTemplate), 1, false, payload)
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
	request.Parameters.DeviceLocation = t.config.DeviceLocation
	request.Parameters.SerialNumber = t.config.SerialNumber
	return request
}

// func (t *Thing) OnConnect() mqtt.OnConnectHandler {
// 	return func(client mqtt.Client) {
// 		fmt.Println("Running MQTT.OnConnectHandler")
// 		t.isConnected = true
// 	}
// }

func createMQTTClient(config *ThingConfiguration) (mqtt.Client, error) {
	tlsCert, err := tls.LoadX509KeyPair(config.KeyPair.CertificatePath, config.KeyPair.PrivateKeyPath)

	if err != nil {
		return nil, fmt.Errorf("failed to load certs: %v", err)
	}

	certs := x509.NewCertPool()

	caPEM, err := ioutil.ReadFile(config.KeyPair.CACertificatePath)

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

	serverURL := fmt.Sprintf("tcps://%s:%v", config.Endpoint, config.Port)
	fmt.Printf("Connecting to %s\n", serverURL)

	mqttOpts := mqtt.NewClientOptions()
	mqttOpts.AddBroker(serverURL)
	mqttOpts.CleanSession = false
	mqttOpts.AutoReconnect = true
	mqttOpts.SetMaxReconnectInterval(1 * time.Second)
	mqttOpts.SetClientID(config.ThingName)
	mqttOpts.SetTLSConfig(tlsConfig)

	return mqtt.NewClient(mqttOpts), nil
}

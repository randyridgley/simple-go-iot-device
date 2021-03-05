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
	"fmt"
	"os"

	"github.com/randyridgley/simple-go-iot-device/device/connect"
)

type Thing struct {
	Connection  connect.Connection
	Config      ThingConfiguration
	isConnected bool
}

type ThingConfiguration struct {
	ThingName            string
	DeviceLocation       string
	SerialNumber         string
	ProvisioningTemplate string
	Endpoint             string
	Port                 int
}

func New(config ThingConfiguration) (*Thing, error) {
	return &Thing{
		Config: config,
	}, nil
}

func (t *Thing) Connect(kp connect.KeyPair) error {
	conf := connect.ConnectionConfiguration{
		KeyPair:  kp,
		Endpoint: t.Config.Endpoint,
		Port:     t.Config.Port,
		ClientId: t.Config.ThingName,
	}
	c, err := connect.New(&conf)
	c.Connect()
	if err != nil {
		return fmt.Errorf("Could not create connection %v", err)
	}
	t.Connection = c
	fmt.Printf("Connected to %s\n", t.Config.Endpoint)
	return nil
}

func (t *Thing) IsProvisioned() bool {
	for _, file := range []string{
		"certs/root.ca.bundle.pem",
		fmt.Sprintf("certs/%s.certificate.pem", t.Config.ThingName),
		fmt.Sprintf("certs/%s.private.key", t.Config.ThingName),
	} {
		_, err := os.Stat(file)
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

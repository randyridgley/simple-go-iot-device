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
package config

type Configurations struct {
	Server         ServerConfigurations
	Bootstrap      BootstrapConfigurations
	SerialNumber   string
	DeviceLocation string
}

// ServerConfigurations exported
type ServerConfigurations struct {
	Port     int
	Endpoint string
}

// DatabaseConfigurations exported
type BootstrapConfigurations struct {
	PrivateKeyPath       string
	CertificatePath      string
	CACertificatePath    string
	ProvisioningTemplate string
}

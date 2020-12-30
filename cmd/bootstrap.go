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
package cmd

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"

	homedir "github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/randyridgley/simple-go-iot-device/config"
	"github.com/randyridgley/simple-go-iot-device/device"
	"github.com/randyridgley/simple-go-iot-device/device/connect"
	"github.com/randyridgley/simple-go-iot-device/device/provision"
	"github.com/randyridgley/simple-go-iot-device/device/shadow"
)

var configuration config.Configurations

type sampleStruct struct {
	Values []int
}

type sampleState struct {
	Value  int
	Struct sampleStruct
}

// bootstrapCmd represents the bootstrap command
var bootstrapCmd = &cobra.Command{
	Use:   "bootstrap",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		err := viper.Unmarshal(&configuration)
		if err != nil {
			fmt.Printf("Unable to decode into struct, %v", err)
		}
		quit := make(chan struct{})
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		thingName := generateThingName(configuration.SerialNumber)

		thingConfig := device.ThingConfiguration{
			ThingName:            thingName,
			DeviceLocation:       configuration.DeviceLocation,
			SerialNumber:         configuration.SerialNumber,
			ProvisioningTemplate: configuration.Bootstrap.ProvisioningTemplate,
			Endpoint:             configuration.Server.Endpoint,
			Port:                 configuration.Server.Port,
		}

		thing, err := device.New(thingConfig)
		check(err)
		fmt.Printf("Created thing")

		if !thing.IsProvisioned() {
			fmt.Println("Provisioning Thing")
			keyPair := connect.KeyPair{
				PrivateKeyPath:    configuration.Bootstrap.PrivateKeyPath,
				CertificatePath:   configuration.Bootstrap.CertificatePath,
				CACertificatePath: configuration.Bootstrap.CACertificatePath,
			}

			p, err := provision.New(*thing, keyPair) // all this looks messy fix
			if err != nil {
				panic(err)
			}
			result, err := p.Provision()
			if !result {
				panic(err)
			}
		}

		keyPair := connect.KeyPair{
			PrivateKeyPath:    configuration.Primary.PrivateKeyPath,
			CertificatePath:   configuration.Primary.CertificatePath,
			CACertificatePath: configuration.Bootstrap.CACertificatePath, //Bootstrap and Primary are same CA
		}

		fmt.Printf("%s\n", keyPair.CertificatePath)
		fmt.Printf("%s\n", keyPair.PrivateKeyPath)
		thing.Connect(keyPair)

		// result := <-thing.Channels.ConnectedChan

		// if result {
		// register device shadow
		// startup and services and topic subscriptions
		payload := "{\"let-me\": \"in\"}"
		thing.Connection.Publish("fleet/2974685", payload)
		fmt.Println("Published message")

		s, err := shadow.New(*thing)
		if err != nil {
			panic(err)
		}
		fmt.Println("Shadow created")

		s.OnError(func(err error) {
			fmt.Printf("async error: %v\n", err)
		})
		s.OnDelta(func(delta map[string]interface{}) {
			fmt.Printf("delta: %+v\n", delta)
		})

		fmt.Println("Registered Error and Delta Handlers")

		fmt.Print("> update desire\n")
		doc, err := s.Desire(sampleState{Value: 1, Struct: sampleStruct{Values: []int{1, 2}}})
		if err != nil {
			panic(err)
		}
		fmt.Printf("document: %+v\n", doc)

		fmt.Print("> update report\n")
		doc, err = s.Report(sampleState{Value: 2, Struct: sampleStruct{Values: []int{1, 2}}})
		if err != nil {
			panic(err)
		}
		fmt.Printf("document: %+v\n", doc)

		fmt.Print("> get document\n")
		doc, err = s.Get()
		if err != nil {
			panic(err)
		}
		fmt.Printf("document: %+v\n", doc)
		// } else {
		// 	fmt.Println("Did not connect successfully")
		// 	quit <- struct{}{}
		// }

		go func() {
			<-c
			// add full cleanup here
			// thing.Cleanup()
			thing.Connection.Disconnect(250)
			fmt.Println("[MQTT] Disconnected")
			quit <- struct{}{}
		}()
		<-quit
	},
}

func init() {
	rootCmd.AddCommand(bootstrapCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// bootstrapCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// bootstrapCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

func generateThingName(serialNumber string) string {
	home, err := homedir.Dir()
	check(err)

	path := fmt.Sprintf("%s/%s", home, "THINGNAME")
	if _, err := os.Stat(path); err == nil {
		f, err := os.Open(path)
		if err != nil {
			fmt.Println(err)
		}
		defer f.Close()
		r := bufio.NewReaderSize(f, 4*1024)
		line, _, err := r.ReadLine()
		return string(line)
	} else {
		d1 := []byte("fleety_" + serialNumber)
		err := os.MkdirAll(home, 0700)
		check(err)
		err = ioutil.WriteFile(path, d1, 0644)
		check(err)
		return string(d1)
	}
}

func check(e error) {
	if e != nil {
		panic(e)
	}
}

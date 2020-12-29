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
	"github.com/segmentio/ksuid"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/randyridgley/simple-go-iot-device/config"
	"github.com/randyridgley/simple-go-iot-device/device"
)

var configuration config.Configurations

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
		viper.BindPFlag("author", rootCmd.PersistentFlags().Lookup("author"))
		fmt.Println("bootstrap called " + rootCmd.PersistentFlags().Lookup("author").Value.String())

		err := viper.Unmarshal(&configuration)
		if err != nil {
			fmt.Printf("Unable to decode into struct, %v", err)
		}

		thingConfig := device.ThingConfiguration{
			ThingName: generateThingName(),
			KeyPair: device.KeyPair{
				PrivateKeyPath:    configuration.Bootstrap.PrivateKeyPath,
				CertificatePath:   configuration.Bootstrap.CertificatePath,
				CACertificatePath: configuration.Bootstrap.CACertificatePath,
			},
			DeviceLocation:       configuration.DeviceLocation,
			SerialNumber:         configuration.SerialNumber,
			ProvisioningTemplate: configuration.Bootstrap.ProvisioningTemplate,
			Endpoint:             configuration.Server.Endpoint,
			Port:                 configuration.Server.Port,
		}

		thing, err := device.NewThing(thingConfig)
		check(err)

		thing.Connect()

		quit := make(chan struct{})
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		go func() {
			<-c
			thing.Client.Disconnect(250)
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

func generateThingName() string {
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
		d1 := []byte("iot_" + ksuid.New().String())
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

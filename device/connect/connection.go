package connect

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type Connection interface {
	// Connect to MQTT server
	Connect() error
	// Disconnect from MQTT Server
	Disconnect(timeout uint)

	Publish(topic string, payload interface{}) mqtt.Token

	Subscribe(topic string, handler mqtt.MessageHandler) error
}

type ConnectionConfiguration struct {
	KeyPair  KeyPair
	Endpoint string
	Port     int
	ClientId string
}

type connection struct {
	Config ConnectionConfiguration
	Client mqtt.Client
}

type KeyPair struct {
	PrivateKeyPath    string
	CertificatePath   string
	CACertificatePath string
}

func New(config *ConnectionConfiguration) (Connection, error) {
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
	fmt.Printf("Preparing connection to %s\n", serverURL)

	mqttOpts := mqtt.NewClientOptions()
	mqttOpts.AddBroker(serverURL)
	mqttOpts.CleanSession = false
	mqttOpts.AutoReconnect = true
	mqttOpts.SetMaxReconnectInterval(1 * time.Second)
	mqttOpts.SetClientID(config.ClientId)
	mqttOpts.SetTLSConfig(tlsConfig)

	c := mqtt.NewClient(mqttOpts)
	conn := &connection{
		Client: c,
		Config: *config,
	}
	return conn, nil
}

func (c *connection) Connect() error {
	// connect to MQTT endpoint
	if token := c.Client.Connect(); token.Wait() && token.Error() != nil {
		fmt.Printf("%v", token.Error())
		return token.Error()
	}
	return nil
}

func (c *connection) Disconnect(timeout uint) {
	c.Client.Disconnect(timeout)
}

func (c *connection) Publish(topic string, payload interface{}) mqtt.Token {
	fmt.Printf("Publishing %v to %v", payload, topic)
	return c.Client.Publish(topic, 1, false, payload)
}

func (c *connection) Subscribe(topic string, handler mqtt.MessageHandler) error {
	fmt.Printf("Subscribing to %v\n", topic)
	if token := c.Client.Subscribe(topic, 1, handler); token.Error() != nil {
		return fmt.Errorf("registering message handlers %v", token.Error())
	}
	return nil
}

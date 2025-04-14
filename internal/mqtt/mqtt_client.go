package mqtt

import (
	"fmt"
	"log"
	"strings"

	MQTT "github.com/eclipse/paho.mqtt.golang"
)

// Client wraps the Paho MQTT client and related settings.
type Client struct {
	pahoClient MQTT.Client
	debug      bool
	user       string
	pw         string
}

// NewClient initializes a new MQTT client struct.
func NewClient(brokerURL, clientID string, debug bool) *Client {
	return &Client{
		debug: debug,
	}
}

// Connect tries to connect to the MQTT broker.
// If brokerURL includes "@" with user:pass, it will parse them automatically.
func (c *Client) Connect(brokerURL, clientID string) error {
	var user, pw string

	if strings.Contains(brokerURL, "@") {
		userPasswordHost := strings.TrimPrefix(brokerURL, "tcp://")
		userPassword, host, found := strings.Cut(userPasswordHost, "@")
		if !found {
			return fmt.Errorf("invalid MQTT URL: %s", brokerURL)
		}
		user, pw, found = strings.Cut(userPassword, ":")
		if !found {
			return fmt.Errorf("invalid user:pass in: %s", userPassword)
		}
		brokerURL = "tcp://" + host
	}
	c.user = user
	c.pw = pw

	opts := MQTT.NewClientOptions().AddBroker(brokerURL).SetClientID(clientID)

	// Set credentials if found
	if c.user != "" || c.pw != "" {
		opts.SetUsername(c.user)
		opts.SetPassword(c.pw)
	}

	// Set the default callback for incoming messages
	opts.SetDefaultPublishHandler(c.defaultHandler)

	c.pahoClient = MQTT.NewClient(opts)
	if token := c.pahoClient.Connect(); token.Wait() && token.Error() != nil {
		return token.Error()
	}

	if c.debug {
		log.Printf("MQTT connected to %s", brokerURL)
	}
	return nil
}

// defaultHandler is called whenever a message is received on a
// subscribed topic that does not have its own callback.
func (c *Client) defaultHandler(_ MQTT.Client, msg MQTT.Message) {
	fmt.Printf("[Default Handler] Topic: %s | Message: %s\n",
		msg.Topic(), string(msg.Payload()))
}

// Subscribe adds a subscription to the given topic.
func (c *Client) Subscribe(topic string) error {
	if token := c.pahoClient.Subscribe(topic, 0, nil); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	if c.debug {
		log.Printf("Subscribed to topic: %s", topic)
	}
	return nil
}

// Unsubscribe removes the subscription from the given topic.
func (c *Client) Unsubscribe(topic string) error {
	if token := c.pahoClient.Unsubscribe(topic); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	if c.debug {
		log.Printf("Unsubscribed from topic: %s", topic)
	}
	return nil
}

// Publish sends a message to the specified topic.
func (c *Client) Publish(topic, payload string) error {
	if c.debug {
		log.Printf("Publishing -> topic=%s msg=%s", topic, payload)
	}
	token := c.pahoClient.Publish(topic, 0, false, payload)
	token.Wait()
	return token.Error()
}

// Disconnect gracefully closes the connection to the MQTT broker.
func (c *Client) Disconnect() {
	if c.pahoClient != nil && c.pahoClient.IsConnected() {
		c.pahoClient.Disconnect(250) // wait 250 ms
	}
}

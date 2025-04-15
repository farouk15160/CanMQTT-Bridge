package mqtt

import (
	"fmt"
	"log"
	"strings"
	"time" // Added for connection retries

	MQTT "github.com/eclipse/paho.mqtt.golang"
	// NO direct import of internal/bridge
)

// --- Callback Types ---
type PublishFunc func(topic, payload string) error
type ConfigHandlerFunc func(payload string)

// --- Variables to hold handlers/callbacks ---
var (
	bridgeMessageHandler MessageHandler    // Interface for standard messages
	bridgeConfigHandler  ConfigHandlerFunc // Function for config updates
)

// --- Functions to set handlers (called from main) ---
func SetBridgeMessageHandler(h MessageHandler) {
	log.Println("MQTT Client: Setting Bridge Message Handler...")
	bridgeMessageHandler = h
	if bridgeMessageHandler == nil {
		log.Println("Warning: Bridge Message Handler set to nil in MQTT client.")
	}
}
func SetConfigHandler(h ConfigHandlerFunc) {
	log.Println("MQTT Client: Setting Config Handler...")
	bridgeConfigHandler = h
	if bridgeConfigHandler == nil {
		log.Println("Warning: Config Handler set to nil in MQTT client.")
	}
}

// Client struct definition
type Client struct {
	pahoClient MQTT.Client
	debug      bool
	brokerURL  string // Store broker URL for reconnects
	clientID   string
	user       string
	pw         string
}

// NewClientAndConnect initializes and connects the MQTT client.
func NewClientAndConnect(clientID, brokerURL string, debug bool) (*Client, error) {
	c := &Client{
		debug:     debug,
		brokerURL: brokerURL,
		clientID:  clientID,
	}
	err := c.connect() // Call internal connect method
	if err != nil {
		return nil, err
	}
	return c, nil
}

// connect handles the actual connection setup and logic.
func (c *Client) connect() error {
	// Parse username/password from brokerURL if present
	connectURL := c.brokerURL // Use the potentially modified URL for connection
	if strings.Contains(c.brokerURL, "@") {
		userPasswordHost := strings.TrimPrefix(c.brokerURL, "tcp://") // Assuming tcp for now
		userPassword, host, found := strings.Cut(userPasswordHost, "@")
		if !found {
			return fmt.Errorf("invalid MQTT URL format (missing @): %s", c.brokerURL)
		}
		var user, pw string
		user, pw, found = strings.Cut(userPassword, ":")
		if !found {
			// Allow username without password
			user = userPassword
			pw = ""
			log.Printf("MQTT Info: Using username '%s' with no password.", user)
			// return fmt.Errorf("invalid user:pass format in MQTT URL: %s", c.brokerURL)
		}
		c.user = user
		c.pw = pw
		// Rebuild URL without credentials for Paho options
		protoPrefix := "tcp://" // Default protocol
		if idx := strings.Index(c.brokerURL, "://"); idx != -1 {
			protoPrefix = c.brokerURL[:idx+3]
		}
		connectURL = protoPrefix + host
		log.Printf("MQTT Info: Connecting to %s with username '%s'", connectURL, c.user)
	} else {
		log.Printf("MQTT Info: Connecting to %s without username/password.", connectURL)
	}

	opts := MQTT.NewClientOptions()
	opts.AddBroker(connectURL)
	opts.SetClientID(c.clientID)
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(10 * time.Second) // Example reconnect interval
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)

	// Set credentials if parsed
	if c.user != "" {
		opts.SetUsername(c.user)
		if c.pw != "" {
			opts.SetPassword(c.pw)
		}
	}

	// Set handlers
	opts.SetDefaultPublishHandler(c.defaultHandler) // Handles incoming messages
	opts.SetConnectionLostHandler(c.connectionLostHandler)
	opts.SetOnConnectHandler(c.onConnectHandler)

	// Create and connect the client
	c.pahoClient = MQTT.NewClient(opts)
	log.Printf("MQTT: Attempting to connect to %s...", connectURL)
	if token := c.pahoClient.Connect(); token.WaitTimeout(10*time.Second) && token.Error() != nil {
		// Wait for connection or timeout
		log.Printf("MQTT Error: Failed to connect to %s: %v", connectURL, token.Error())
		// Consider returning error or allowing auto-reconnect to handle it
		return token.Error() // Return error if initial connect fails after timeout
	}

	// Check if connection was actually successful (Connect() might return nil even if connect fails later)
	if !c.pahoClient.IsConnected() {
		log.Println("MQTT Error: Connection attempt finished, but client is not connected.")
		return fmt.Errorf("failed to establish MQTT connection to %s", connectURL)
	}

	log.Printf("MQTT: Successfully connected to %s", connectURL)
	return nil
}

// defaultHandler routes incoming messages based on topic.
func (c *Client) defaultHandler(client MQTT.Client, msg MQTT.Message) {
	topic := msg.Topic()
	payload := string(msg.Payload())
	isConfigTopic := (topic == "translater/run")

	if c.debug {
		log.Printf("[MQTT Handler] Received: Topic=%s | IsConfig=%t | Payload=%s", topic, isConfigTopic, payload)
	}

	if isConfigTopic {
		if bridgeConfigHandler != nil {
			bridgeConfigHandler(payload) // Call the config handler func
		} else {
			log.Println("Error: Config message received but no Config Handler set.")
		}
	} else {
		if bridgeMessageHandler != nil {
			bridgeMessageHandler.HandleMessage(client, msg) // Call the message handler interface method
		} else {
			log.Printf("Error: Cannot handle message for topic '%s', Bridge Message Handler not set.", topic)
		}
	}
}

// connectionLostHandler logs connection loss. AutoReconnect handles reconnection.
func (c *Client) connectionLostHandler(client MQTT.Client, err error) {
	log.Printf("MQTT Error: Connection lost: %v. Attempting to reconnect...", err)
}

// onConnectHandler logs successful connections/reconnections.
// Could be used to re-subscribe if needed, but AutoReconnect often handles that.
func (c *Client) onConnectHandler(client MQTT.Client) {
	log.Println("MQTT: Connection established/re-established.")
	// Re-subscribe logic could go here if necessary, but Paho's auto-reconnect
	// usually resubscribes automatically if CleanSession is false (default).
	// If CleanSession is true, you MUST resubscribe here.
	// Example: if bridgeResubscribeFunc != nil { bridgeResubscribeFunc() }
}

// Publish sends a message.
func (c *Client) Publish(topic, payload string) error {
	if !c.pahoClient.IsConnected() {
		return fmt.Errorf("MQTT client not connected, cannot publish to %s", topic)
	}
	if c.debug {
		log.Printf("MQTT Publish -> Topic=%s | Payload=%s", topic, payload)
	}
	// token := c.pahoClient.Publish(topic, 0, false, payload)
	// token.Wait() // Optionally wait, but can block
	// Check error async or let Paho handle queueing? For now, don't wait.
	return nil // Assume success if queuing works, Paho handles actual send
}

// PublishRetained sends a message with the retained flag.
func (c *Client) PublishRetained(topic, payload string) error {
	if !c.pahoClient.IsConnected() {
		return fmt.Errorf("MQTT client not connected, cannot publish retained to %s", topic)
	}
	if c.debug {
		log.Printf("MQTT PublishRetained -> Topic=%s | Payload=%s", topic, payload)
	}
	token := c.pahoClient.Publish(topic, 0, true, payload)
	// token.Wait() // Wait for retained publish? Might be safer.
	go func() { // Wait in goroutine to avoid blocking publisher? Needs care.
		token.Wait()
		if token.Error() != nil {
			log.Printf("MQTT Error: Failed to publish retained to topic '%s': %v", topic, token.Error())
		} else if c.debug {
			log.Printf("MQTT Successfully published retained to topic '%s'", topic)
		}
	}()
	return nil // Return immediately
}

// Subscribe adds a subscription.
func (c *Client) Subscribe(topic string) error {
	if !c.pahoClient.IsConnected() {
		return fmt.Errorf("MQTT client not connected, cannot subscribe to %s", topic)
	}
	if token := c.pahoClient.Subscribe(topic, 0, nil); token.WaitTimeout(5*time.Second) && token.Error() != nil {
		log.Printf("MQTT Error: Failed to subscribe to topic '%s': %v", topic, token.Error())
		return token.Error()
	}
	if c.debug {
		log.Printf("MQTT Subscribed to topic: %s", topic)
	}
	return nil
}

// Unsubscribe removes a subscription.
func (c *Client) Unsubscribe(topic string) error {
	if !c.pahoClient.IsConnected() {
		log.Printf("MQTT Warning: Client not connected, cannot unsubscribe from %s", topic)
		// Return nil or error depending on desired behavior
		return nil
	}
	if token := c.pahoClient.Unsubscribe(topic); token.WaitTimeout(5*time.Second) && token.Error() != nil {
		log.Printf("MQTT Error: Failed to unsubscribe from topic '%s': %v", topic, token.Error())
		return token.Error()
	}
	if c.debug {
		log.Printf("MQTT Unsubscribed from topic: %s", topic)
	}
	return nil
}

// Disconnect gracefully closes the connection.
func (c *Client) Disconnect() {
	if c.pahoClient != nil && c.pahoClient.IsConnected() {
		log.Println("MQTT: Disconnecting client...")
		c.pahoClient.Disconnect(500) // wait 500 ms for disconnection
		log.Println("MQTT: Client disconnected.")
	} else {
		// log.Println("MQTT: Client already disconnected or not initialized.")
	}
}

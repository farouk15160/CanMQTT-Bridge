// internal/mqtt/mqtt_client.go
package mqtt

import (
	// Added for status handler payload
	// Added for status handler formatting
	"log"
	"strings"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	// Import bridge package ONLY to access the exported rule map and lock
	// Avoids circular dependency if bridge doesn't import mqtt
	"github.com/farouk15160/Translater-code-new/internal/bridge"
	// NO direct import of internal/config
)

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

// NewClientAndConnect initializes the MQTT client and initiates the connection process.
func NewClientAndConnect(clientID, brokerURL string, debug bool) (*Client, error) {
	c := &Client{
		debug:     debug,
		brokerURL: brokerURL,
		clientID:  clientID,
	}
	c.connect()
	return c, nil
}

// connect configures Paho options and starts the connection attempt.
func (c *Client) connect() {
	connectURL := c.brokerURL
	// --- Parse username/password ---
	if strings.Contains(c.brokerURL, "@") {
		userPasswordHost := strings.TrimPrefix(c.brokerURL, "tcp://")
		userPassword, host, found := strings.Cut(userPasswordHost, "@")
		if !found {
			log.Printf("MQTT Error: Invalid MQTT URL format: %s. Proceeding without credentials.", c.brokerURL)
		} else {
			var user, pw string
			user, pw, found = strings.Cut(userPassword, ":")
			if !found {
				user = userPassword
				pw = ""
			}
			c.user = user
			c.pw = pw
			protoPrefix := "tcp://"
			if idx := strings.Index(c.brokerURL, "://"); idx != -1 {
				protoPrefix = c.brokerURL[:idx+3]
			}
			connectURL = protoPrefix + host
			log.Printf("MQTT Info: Will connect to %s with username '%s'", connectURL, c.user)
		}
	} else {
		log.Printf("MQTT Info: Will connect to %s without username/password.", connectURL)
	}
	// --- End Parse username/password ---

	opts := MQTT.NewClientOptions()
	opts.AddBroker(connectURL)
	opts.SetClientID(c.clientID)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetMaxReconnectInterval(1 * time.Minute)
	opts.SetOrderMatters(false) // Allow out-of-order processing for performance

	// *** Important for ensuring subscriptions survive short disconnects ***
	// If CleanSession is false, the broker remembers subscriptions for this clientID.
	// Paho's ResumeSubs might then work automatically. Setting both often desired.
	opts.SetCleanSession(false) // Set to false to persist session
	opts.SetResumeSubs(true)    // Ask Paho to resubscribe remembered subscriptions

	if c.user != "" {
		opts.SetUsername(c.user)
	}
	if c.pw != "" {
		opts.SetPassword(c.pw)
	}

	opts.SetDefaultPublishHandler(c.defaultHandler)
	opts.SetConnectionLostHandler(c.connectionLostHandler)
	opts.SetOnConnectHandler(c.onConnectHandler) // Setup the callback

	c.pahoClient = MQTT.NewClient(opts)

	log.Printf("MQTT: Initiating connection to %s (will retry automatically)...", connectURL)
	go func() {
		if token := c.pahoClient.Connect(); token.Wait() && token.Error() != nil {
			log.Printf("MQTT: Initial connection attempt failed: %v (AutoReconnect enabled)", token.Error())
		}
	}()
}

// defaultHandler routes incoming messages based on topic.
// It now launches message processing in separate goroutines.
func (c *Client) defaultHandler(client MQTT.Client, msg MQTT.Message) {
	topic := msg.Topic()
	payloadCopy := make([]byte, len(msg.Payload()))
	copy(payloadCopy, msg.Payload())
	receivedTime := time.Now()

	if c.debug {
		logPayloadStr := string(payloadCopy)
		if len(logPayloadStr) > 100 {
			logPayloadStr = logPayloadStr[:100] + "..."
		}
		log.Printf("[MQTT Handler] Received: Topic=%s | Payload=%s", topic, logPayloadStr) // THIS IS THE LOG TO LOOK FOR
	}

	// Launch processing in a goroutine
	go func(t string, p []byte, rTime time.Time) {
		processingStartTime := time.Now()
		switch t {
		case "translater/run": // Internal config update topic
			if bridgeConfigHandler != nil {
				bridgeConfigHandler(string(p))
			} else {
				log.Println("Warning: Received message on 'translater/run' but no Config Handler is set.")
			}
		case "translater/process": // Internal status request topic
			// Need to pass the Client instance 'c' to publish the response
			handleTranslatorStatus(t, string(p), c) // Pass client instance
		default:
			// Assume it's a message for the bridge (MQTT -> CAN)
			if bridgeMessageHandler != nil {
				// Pass details to the bridge handler (which queues it for workers)
				bridgeMessageHandler.HandleMessage(client, &simpleMessage{topic: t, payload: p})
			} else if c.debug {
				log.Printf("Debug: No Bridge Message Handler set for topic '%s'. Message ignored.", t)
			}
		}
		// Log processing time
		processingDuration := time.Since(processingStartTime)
		totalDuration := time.Since(rTime)
		// Reduce log verbosity? Only log if duration > threshold?
		log.Printf("[Perf] MQTT message (Topic: %s) processing time: %v (Total time since reception: %v)", t, processingDuration, totalDuration)

	}(topic, payloadCopy, receivedTime)
}

// --- onConnectHandler: Modified to resubscribe Bridge topics ---
func (c *Client) onConnectHandler(client MQTT.Client) {
	log.Println("MQTT: Connection established/re-established.")

	// --- Re-subscribe to internal command topics ---
	log.Println("MQTT: Re-subscribing to internal command topics...")
	internalTopics := map[string]byte{
		"translater/process": 0, // QoS 0
		"translater/run":     0, // QoS 0
	}
	for topic, qos := range internalTopics {
		if err := c.subscribeInternal(topic, qos); err != nil {
			log.Printf("Error re-subscribing to internal topic %s: %v", topic, err)
		}
	}

	// --- Re-subscribe to MQTT->CAN topics from bridge config ---
	log.Println("MQTT: Re-subscribing to bridge MQTT->CAN topics...")
	bridge.ConfigLock.RLock() // Lock bridge config for reading MqttRuleMap
	topicsToSubscribe := make([]string, 0, len(bridge.MqttRuleMap))
	for topic := range bridge.MqttRuleMap {
		topicsToSubscribe = append(topicsToSubscribe, topic)
	}
	bridge.ConfigLock.RUnlock() // Unlock

	if len(topicsToSubscribe) > 0 {
		log.Printf("MQTT: Found %d bridge topics to re-subscribe...", len(topicsToSubscribe))
		for _, topic := range topicsToSubscribe {
			// Using QoS 0 for all bridge topics for now
			if err := c.subscribeInternal(topic, 0); err != nil {
				log.Printf("Error re-subscribing to bridge topic %s: %v", topic, err)
			}
		}
	} else {
		log.Println("MQTT: No bridge MQTT->CAN topics found in current config to re-subscribe.")
	}

	log.Println("MQTT: Re-subscription process completed.")
}

// --- End onConnectHandler ---

// subscribeInternal helper
func (c *Client) subscribeInternal(topic string, qos byte) error {
	if !c.IsConnected() {
		log.Printf("MQTT Warning: Client not connected when trying to subscribe to %s.", topic)
		// Don't return error immediately, let Paho handle it if possible
	}
	// Increased timeout for subscription acknowledgement
	if token := c.pahoClient.Subscribe(topic, qos, nil); token.WaitTimeout(10*time.Second) && token.Error() != nil {
		// Log detailed error from Paho
		log.Printf("MQTT Error: Failed to subscribe to topic '%s': %v", topic, token.Error())
		return token.Error()
	}
	log.Printf("MQTT: Subscribed to topic '%s'", topic)
	return nil
}

// Publish sends a non-retained message (QoS 0).
func (c *Client) Publish(topic, payload string) error {
	if !c.IsConnected() {
		log.Printf("MQTT Warning: Client not connected when publishing to %s. Message might be lost.", topic)
		// Return error or allow Paho to queue? For QoS 0, loss is possible anyway.
		// return fmt.Errorf("not connected")
	}
	if c.debug {
		logPayload := payload
		if len(logPayload) > 100 {
			logPayload = logPayload[:100] + "..."
		}
		log.Printf("MQTT Publish -> Topic=%s | Payload=%s", topic, logPayload)
	}
	token := c.pahoClient.Publish(topic, 0, false, payload)
	// Fire-and-forget for QoS 0, but log errors async if possible
	go func(t MQTT.Token, top string) {
		_ = t.WaitTimeout(1 * time.Second) // Don't block, just check briefly
		if err := t.Error(); err != nil {
			log.Printf("MQTT Error: Async check for publish to topic '%s' failed: %v", top, err)
		}
	}(token, topic)
	return nil // Return immediately
}

// PublishRetained sends a message with the retained flag set (QoS 0).
func (c *Client) PublishRetained(topic, payload string) error {
	if !c.IsConnected() {
		log.Printf("MQTT Warning: Client not connected when publishing retained to %s.", topic)
		// return fmt.Errorf("not connected")
	}
	if c.debug {
		logPayload := payload
		if len(logPayload) > 100 {
			logPayload = logPayload[:100] + "..."
		}
		log.Printf("MQTT PublishRetained -> Topic=%s | Payload=%s", topic, logPayload)
	}
	token := c.pahoClient.Publish(topic, 0, true, payload)
	// Async check for retained messages
	go func(t MQTT.Token, top string) {
		if t.WaitTimeout(3*time.Second) && t.Error() != nil {
			log.Printf("MQTT Error: Async check for publish RETAINED to topic '%s' failed: %v", top, t.Error())
		} else if c.debug && t.Error() == nil {
			log.Printf("MQTT Publish RETAINED confirmed for topic '%s'", top) // Log confirmation only if debug
		}
	}(token, topic)
	return nil // Return immediately
}

// Subscribe adds a subscription (QoS 0). Public API method.
func (c *Client) Subscribe(topic string) error {
	return c.subscribeInternal(topic, 0)
}

// Unsubscribe removes a subscription. Public API method.
func (c *Client) Unsubscribe(topic string) error {
	if !c.IsConnected() {
		log.Printf("MQTT Warning: Client not connected, cannot unsubscribe from %s", topic)
		return nil // Or return error: fmt.Errorf("MQTT client not connected")
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
	if c.pahoClient != nil && c.IsConnected() {
		log.Println("MQTT: Disconnecting client...")
		c.pahoClient.Disconnect(500) // wait 500 ms
		log.Println("MQTT: Client disconnected.")
	} else {
		// log.Println("MQTT: Client already disconnected or not initialized.") // Reduce noise
	}
}

// IsConnected checks connection status.
func (c *Client) IsConnected() bool {
	return c.pahoClient != nil && c.pahoClient.IsConnected()
}

// --- Handlers called by defaultHandler ---

// handleTranslatorStatus (moved from handlers.go to keep it together with client logic for now)
// Gathers system info and publishes to "translater/status"

// --- simpleMessage & other helpers from original mqtt_client.go ---

// simpleMessage is used to pass message data to goroutines safely if needed
// (Currently bridge.HandleMessage takes the original msg, maybe adapt later)
type simpleMessage struct {
	dup       bool
	qos       byte
	retained  bool
	topic     string
	messageID uint16
	payload   []byte
	ack       func()
}

func (m *simpleMessage) Duplicate() bool   { return m.dup }
func (m *simpleMessage) Qos() byte         { return m.qos }
func (m *simpleMessage) Retained() bool    { return m.retained }
func (m *simpleMessage) Topic() string     { return m.topic }
func (m *simpleMessage) MessageID() uint16 { return m.messageID }
func (m *simpleMessage) Payload() []byte   { return m.payload }
func (m *simpleMessage) Ack() {
	if m.ack != nil {
		m.ack()
	}
}

// connectionLostHandler logs connection loss. AutoReconnect handles reconnection.
func (c *Client) connectionLostHandler(client MQTT.Client, err error) {
	log.Printf("MQTT Error: Connection lost: %v. AutoReconnect will attempt to reconnect...", err)
}

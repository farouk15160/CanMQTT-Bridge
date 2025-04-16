package mqtt

import (
	"log"
	"strings"
	"time" // Added for connection retries

	MQTT "github.com/eclipse/paho.mqtt.golang"
	// NO direct import of internal/bridge
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
// It relies on Paho's AutoReconnect feature for resilience.
func NewClientAndConnect(clientID, brokerURL string, debug bool) (*Client, error) {
	c := &Client{
		debug:     debug,
		brokerURL: brokerURL,
		clientID:  clientID,
	}
	// connect() now only sets up options and starts the connection attempt.
	// It doesn't wait indefinitely or return connection errors directly.
	c.connect()
	// Return the client instance immediately. The connection happens in the background.
	// No error is returned here as Paho handles retries internally.
	return c, nil
}

// connect configures Paho options and starts the connection attempt.
func (c *Client) connect() {
	// Parse username/password from brokerURL if present
	connectURL := c.brokerURL
	if strings.Contains(c.brokerURL, "@") {
		// ... (parsing logic remains the same)
		userPasswordHost := strings.TrimPrefix(c.brokerURL, "tcp://") // Assuming tcp for now
		userPassword, host, found := strings.Cut(userPasswordHost, "@")
		if !found {
			log.Printf("MQTT Error: Invalid MQTT URL format (missing @): %s. Proceeding without credentials.", c.brokerURL)
			connectURL = c.brokerURL // Use original URL if parsing fails badly
		} else {
			var user, pw string
			user, pw, found = strings.Cut(userPassword, ":")
			if !found {
				user = userPassword
				pw = ""
				log.Printf("MQTT Info: Using username '%s' with no password.", user)
			}
			c.user = user
			c.pw = pw
			protoPrefix := "tcp://"
			if idx := strings.Index(c.brokerURL, "://"); idx != -1 {
				protoPrefix = c.brokerURL[:idx+3]
			}
			connectURL = protoPrefix + host // URL for Paho options excludes credentials
			log.Printf("MQTT Info: Will connect to %s with username '%s'", connectURL, c.user)
		}
	} else {
		log.Printf("MQTT Info: Will connect to %s without username/password.", connectURL)
	}

	opts := MQTT.NewClientOptions()
	opts.AddBroker(connectURL)
	opts.SetClientID(c.clientID)

	// *** Configure Paho for automatic reconnection ***
	opts.SetAutoReconnect(true)                   // Enable auto-reconnect
	opts.SetConnectRetry(true)                    // Keep trying to connect if initial connection fails
	opts.SetConnectRetryInterval(5 * time.Second) // Wait 5s between retries
	opts.SetMaxReconnectInterval(1 * time.Minute) // Max wait time between retries after backoff
	// opts.SetConnectTimeout(10 * time.Second) // Timeout for a single connection attempt (optional)

	opts.SetOrderMatters(false) // Allow out-of-order processing

	// Set credentials if parsed
	if c.user != "" {
		opts.SetUsername(c.user)
		if c.pw != "" {
			opts.SetPassword(c.pw)
		}
	}

	// Set handlers
	opts.SetDefaultPublishHandler(c.defaultHandler)
	opts.SetConnectionLostHandler(c.connectionLostHandler)
	opts.SetOnConnectHandler(c.onConnectHandler) // Crucial for re-subscribing

	// Create the client
	c.pahoClient = MQTT.NewClient(opts)

	// *** Start the connection attempt in the background ***
	log.Printf("MQTT: Initiating connection to %s (will retry automatically)...", connectURL)
	// Connect() starts the process; Paho handles retries internally.
	// We don't wait for the token here anymore.
	go func() {
		if token := c.pahoClient.Connect(); token.Wait() && token.Error() != nil {
			// Log initial connection error, but know that Paho will keep trying.
			log.Printf("MQTT: Initial connection attempt failed: %v (AutoReconnect enabled)", token.Error())
		}
	}()
}

// defaultHandler routes incoming messages based on topic.
// It now launches message processing in separate goroutines.
func (c *Client) defaultHandler(client MQTT.Client, msg MQTT.Message) {
	// Capture message details immediately
	topic := msg.Topic()
	// Copy payload as the original buffer might be reused by Paho
	payloadCopy := make([]byte, len(msg.Payload()))
	copy(payloadCopy, msg.Payload())
	receivedTime := time.Now() // Record time of reception

	if c.debug {
		logPayloadStr := string(payloadCopy)
		if len(logPayloadStr) > 100 {
			logPayloadStr = logPayloadStr[:100] + "..."
		}
		log.Printf("[MQTT Handler] Received: Topic=%s | Payload=%s", topic, logPayloadStr)
	}

	// *** Launch processing in a goroutine ***
	go func(t string, p []byte, rTime time.Time) {
		processingStartTime := time.Now()
		// Handle specific internal command topics
		switch t {
		case "translater/run":
			if bridgeConfigHandler != nil {
				bridgeConfigHandler(string(p)) // Pass payload string
			} else {
				log.Println("Warning: Received message on 'translater/run' but no Config Handler is set.")
			}
		case "translater/process":
			handleTranslatorStatus(t, string(p), c) // Pass client for publishing
		default:
			// For all other topics, assume they are for the bridge (MQTT -> CAN)
			if bridgeMessageHandler != nil {
				// Create a temporary message object for the handler if needed,
				// or adapt HandleMessage interface if it doesn't need the client object.
				// Here, we create a simple message struct satisfying the interface implicitly
				// if HandleMessage only needs Topic() and Payload().
				// If HandleMessage needs the full MQTT.Message, we might need a different approach
				// as the original `msg` object might be invalid after defaultHandler returns.
				// Let's assume bridge.HandleMessage can work with topic/payload or a simpler struct.
				// For now, passing the original client and a mock/copied message:
				// WARNING: Passing the original client object to multiple goroutines is safe,
				// but passing the original msg object might not be if Paho reuses it.
				// Safest is to pass topic/payload string/bytes.
				// Let's adapt the bridge handler interface slightly if possible.
				// Assuming HandleMessage can be called like this for now:
				bridgeMessageHandler.HandleMessage(client, &simpleMessage{topic: t, payload: p})

			} else {
				if c.debug {
					log.Printf("Debug: No Bridge Message Handler set for topic '%s'. Message ignored.", t)
				}
			}
		}
		// Log processing time for this MQTT message
		processingDuration := time.Since(processingStartTime)
		totalDuration := time.Since(rTime)
		log.Printf("[Perf] MQTT message (Topic: %s) processing time: %v (Total time since reception: %v)", t, processingDuration, totalDuration)

	}(topic, payloadCopy, receivedTime) // Pass copied data to goroutine
}

// simpleMessage is used to pass message data to goroutines safely
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

// onConnectHandler logs successful connections/reconnections.
// It also re-subscribes to necessary topics upon reconnection.
func (c *Client) onConnectHandler(client MQTT.Client) {
	log.Println("MQTT: Connection established/re-established.")
	// Re-subscribe logic is crucial! Paho might not always resubscribe automatically
	// depending on session state and broker behavior. Explicit resubscribe is safer.
	log.Println("MQTT: Re-subscribing to command topics...")

	// Use a map for topics to manage subscriptions easily
	topicsToSubscribe := map[string]byte{
		"translater/process": 0, // QoS 0
		"translater/run":     0, // QoS 0
		// Add any other essential topics here that aren't managed by the bridge's config reload
	}

	for topic, qos := range topicsToSubscribe {
		if err := c.subscribeInternal(topic, qos); err != nil {
			log.Printf("Error re-subscribing to %s: %v", topic, err)
			// Maybe add retry logic here?
		}
	}

	// Trigger bridge to re-subscribe its topics if necessary
	// This requires adding a callback mechanism from MQTT client back to the bridge/main
	// Example: if bridgeResubscribeFunc != nil { bridgeResubscribeFunc() }
	log.Println("MQTT: Re-subscription process completed.")

}

// subscribeInternal is a helper for subscribing with logging.
func (c *Client) subscribeInternal(topic string, qos byte) error {
	if !c.IsConnected() {
		log.Printf("MQTT Warning: Client not connected when trying to subscribe to %s. Subscription will likely fail until reconnected.", topic)
		// Don't return error, let Paho queue/retry if possible
	}
	// Subscribe can take time, especially if the network is slow or broker busy
	if token := c.pahoClient.Subscribe(topic, qos, nil); token.WaitTimeout(10*time.Second) && token.Error() != nil { // Increased timeout
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
		// Paho might queue if configured, but QoS 0 has no guarantee.
		// Return an error if delivery guarantee is important when disconnected.
		// return fmt.Errorf("MQTT client not connected, cannot publish to %s", topic)
	}
	if c.debug {
		logPayload := payload
		if len(logPayload) > 100 {
			logPayload = logPayload[:100] + "..."
		}
		log.Printf("MQTT Publish -> Topic=%s | Payload=%s", topic, logPayload)
	}
	// Publish with QoS 0, non-retained
	token := c.pahoClient.Publish(topic, 0, false, payload)

	// Fire-and-forget for QoS 0, Paho handles sending. Optional check:
	go func(t MQTT.Token, top string) {
		// Very short timeout just to catch immediate errors if possible
		_ = t.WaitTimeout(1 * time.Second) // Don't really need to wait
		if err := t.Error(); err != nil {
			log.Printf("MQTT Error: Publish to topic '%s' failed: %v", top, err)
		}
	}(token, topic)

	return nil // Return immediately
}

// PublishRetained sends a message with the retained flag set (QoS 0).
func (c *Client) PublishRetained(topic, payload string) error {
	if !c.IsConnected() {
		log.Printf("MQTT Warning: Client not connected when publishing retained to %s. Message might be lost.", topic)
		// return fmt.Errorf("MQTT client not connected, cannot publish retained to %s", topic)
	}
	if c.debug {
		logPayload := payload
		if len(logPayload) > 100 {
			logPayload = logPayload[:100] + "..."
		}
		log.Printf("MQTT PublishRetained -> Topic=%s | Payload=%s", topic, logPayload)
	}
	// Publish with QoS 0, retained=true
	token := c.pahoClient.Publish(topic, 0, true, payload)

	// Optional: Asynchronous error checking for retained messages
	go func(t MQTT.Token, top string) {
		// Wait slightly longer to increase chance of broker processing
		if t.WaitTimeout(3*time.Second) && t.Error() != nil {
			log.Printf("MQTT Error: Publish RETAINED to topic '%s' failed: %v", top, t.Error())
		} else if c.debug && t.Error() == nil {
			log.Printf("MQTT Publish RETAINED confirmed for topic '%s'", top)
		}
	}(token, topic)

	return nil // Return immediately
}

// Subscribe adds a subscription (QoS 0).
func (c *Client) Subscribe(topic string) error {
	// Use internal helper to perform subscription and logging
	return c.subscribeInternal(topic, 0)
}

// Unsubscribe removes a subscription.
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
	// Check if client exists and is connected before disconnecting
	if c.pahoClient != nil && c.IsConnected() {
		log.Println("MQTT: Disconnecting client...")
		c.pahoClient.Disconnect(500) // wait 500 ms
		log.Println("MQTT: Client disconnected.")
	} else {
		log.Println("MQTT: Client already disconnected or not initialized.")
	}
}

// IsConnected checks if the underlying Paho client is initialized and connected.
func (c *Client) IsConnected() bool {
	return c.pahoClient != nil && c.pahoClient.IsConnected()
}

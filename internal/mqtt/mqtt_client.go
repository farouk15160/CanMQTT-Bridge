
package mqtt

import (

	"encoding/json"
	"log"
	"strings"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/farouk15160/Translater-code-new/internal/bridge"

)

var (
	bridgeMessageHandler MessageHandler    
	bridgeConfigHandler  ConfigHandlerFunc 
)

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

func NewClientAndConnect(clientID, brokerURL string, debug bool) (*Client, error) {
	c := &Client{
		debug:     debug,
		brokerURL: brokerURL,
		clientID:  clientID,
	}
	c.connect()
	return c, nil
}
func (c *Client) connect() {
	connectURL := c.brokerURL
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

	opts := MQTT.NewClientOptions()
	opts.AddBroker(connectURL)
	opts.SetClientID(c.clientID)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetMaxReconnectInterval(1 * time.Minute)
	opts.SetOrderMatters(false) 
	opts.SetCleanSession(false) 
	opts.SetResumeSubs(true)    
	if c.user != "" {
		opts.SetUsername(c.user)
	}
	if c.pw != "" {
		opts.SetPassword(c.pw)
	}

	opts.SetDefaultPublishHandler(c.defaultHandler)
	opts.SetConnectionLostHandler(c.connectionLostHandler)
	opts.SetOnConnectHandler(c.onConnectHandler) 

	c.pahoClient = MQTT.NewClient(opts)

	log.Printf("MQTT: Initiating connection to %s (will retry automatically)...", connectURL)
	go func() {
		if token := c.pahoClient.Connect(); token.Wait() && token.Error() != nil {
			log.Printf("MQTT: Initial connection attempt failed: %v (AutoReconnect enabled)", token.Error())
		}
	}()
}

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
		log.Printf("[MQTT Handler] Received: Topic=%s | Payload=%s", topic, logPayloadStr)
	}

	go func(t string, p []byte, rTime time.Time) {
		processingStartTime := time.Now()
		switch t {
		case "translater/run":
			if bridgeConfigHandler != nil {
				bridgeConfigHandler(string(p))
			} else {
				log.Println("Warning: Received message on 'translater/run' but no Config Handler is set.")
			}
		case "translater/process": 
		
			handleTranslatorStatus(t, string(p), c)
		case "translater/clock":
			var clockPayload ClockConfigPayload
			err := json.Unmarshal(p, &clockPayload)
			if err != nil {
				log.Printf("Error unmarshalling JSON for topic '%s': %v", t, err)
			} else if clockPayload.Takt != nil { 
				
				log.Printf("Received clock takt update: %d Hz", *clockPayload.Takt)
				bridge.SetClockTakt(uint8(*clockPayload.Takt)) 
			} else {
				log.Printf("Received message on '%s' but 'takt' key was missing or null.", t)
			}
		
		default:
			if bridgeMessageHandler != nil {
				bridgeMessageHandler.HandleMessage(client, &simpleMessage{topic: t, payload: p})
			} else if c.debug {
				log.Printf("Debug: No Bridge Message Handler set for topic '%s'. Message ignored.", t)
			}
		}
		
		processingDuration := time.Since(processingStartTime)
		totalDuration := time.Since(rTime)
		log.Printf("[Perf] MQTT message (Topic: %s) processing time: %v (Total time since reception: %v)", t, processingDuration, totalDuration)

	}(topic, payloadCopy, receivedTime)
}

func (c *Client) onConnectHandler(client MQTT.Client) {
	log.Println("MQTT: Connection established/re-established.")

	log.Println("MQTT: Re-subscribing to internal command topics...")
	internalTopics := map[string]byte{
		"translater/process": 0, // QoS 0
		"translater/run":     0, // QoS 0
		"translater/clock":   0, 
	}
	for topic, qos := range internalTopics {
		if err := c.subscribeInternal(topic, qos); err != nil {
			log.Printf("Error re-subscribing to internal topic %s: %v", topic, err)
		}
	}

	log.Println("MQTT: Re-subscribing to bridge MQTT->CAN topics...")
	bridge.ConfigLock.RLock() 
	topicsToSubscribe := make([]string, 0, len(bridge.MqttRuleMap))
	for topic := range bridge.MqttRuleMap {
		topicsToSubscribe = append(topicsToSubscribe, topic)
	}
	bridge.ConfigLock.RUnlock()

	if len(topicsToSubscribe) > 0 {
		log.Printf("MQTT: Found %d bridge topics to re-subscribe...", len(topicsToSubscribe))
		for _, topic := range topicsToSubscribe {
			
			if err := c.subscribeInternal(topic, 0); err != nil {
				log.Printf("Error re-subscribing to bridge topic %s: %v", topic, err)
			}
		}
	} else {
		log.Println("MQTT: No bridge MQTT->CAN topics found in current config to re-subscribe.")
	}

	log.Println("MQTT: Re-subscription process completed.")
}

func (c *Client) subscribeInternal(topic string, qos byte) error {
	if !c.IsConnected() {
		log.Printf("MQTT Warning: Client not connected when trying to subscribe to %s.", topic)
	}
	if token := c.pahoClient.Subscribe(topic, qos, nil); token.WaitTimeout(10*time.Second) && token.Error() != nil {
		log.Printf("MQTT Error: Failed to subscribe to topic '%s': %v", topic, token.Error())
		return token.Error()
	}
	log.Printf("MQTT: Subscribed to topic '%s'", topic)
	return nil
}

func (c *Client) Publish(topic, payload string) error {
	if !c.IsConnected() {
		log.Printf("MQTT Warning: Client not connected when publishing to %s. Message might be lost.", topic)
	}
	if c.debug {
		logPayload := payload
		if len(logPayload) > 100 {
			logPayload = logPayload[:100] + "..."
		}
		log.Printf("MQTT Publish -> Topic=%s | Payload=%s", topic, logPayload)
	}
	token := c.pahoClient.Publish(topic, 0, false, payload)
	go func(t MQTT.Token, top string) {
		_ = t.WaitTimeout(1 * time.Second) 
		if err := t.Error(); err != nil {
			log.Printf("MQTT Error: Async check for publish to topic '%s' failed: %v", top, err)
		}
	}(token, topic)
	return nil 
}

func (c *Client) PublishRetained(topic, payload string) error {
	if !c.IsConnected() {
		log.Printf("MQTT Warning: Client not connected when publishing retained to %s.", topic)
	}
	if c.debug {
		logPayload := payload
		if len(logPayload) > 100 {
			logPayload = logPayload[:100] + "..."
		}
		log.Printf("MQTT PublishRetained -> Topic=%s | Payload=%s", topic, logPayload)
	}
	token := c.pahoClient.Publish(topic, 0, true, payload)
	go func(t MQTT.Token, top string) {
		if t.WaitTimeout(3*time.Second) && t.Error() != nil {
			log.Printf("MQTT Error: Async check for publish RETAINED to topic '%s' failed: %v", top, t.Error())
		} else if c.debug && t.Error() == nil {
			log.Printf("MQTT Publish RETAINED confirmed for topic '%s'", top) 
		}
	}(token, topic)
	return nil
}

func (c *Client) Subscribe(topic string) error {
	return c.subscribeInternal(topic, 0)
}
func (c *Client) Unsubscribe(topic string) error {
	if !c.IsConnected() {
		log.Printf("MQTT Warning: Client not connected, cannot unsubscribe from %s", topic)
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

func (c *Client) Disconnect() {
	if c.pahoClient != nil && c.IsConnected() {
		log.Println("MQTT: Disconnecting client...")
		c.pahoClient.Disconnect(500) // wait 500 ms
		log.Println("MQTT: Client disconnected.")
	} else {
		log.Println("MQTT: Client already disconnected or not initialized.") // Reduce noise
	}
}

func (c *Client) IsConnected() bool {
	return c.pahoClient != nil && c.pahoClient.IsConnected()
}
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

func (c *Client) connectionLostHandler(client MQTT.Client, err error) {
	log.Printf("MQTT Error: Connection lost: %v. AutoReconnect will attempt to reconnect...", err)
}

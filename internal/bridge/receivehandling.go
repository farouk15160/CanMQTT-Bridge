package bridge

import (
	"encoding/json"
	"log"
	"strconv"
	"strings"

	"github.com/brutella/can"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	// Import config package if needed for struct types directly
	// "github.com/farouk15160/Translater-code-new/internal/config"
)

// --- Callback Type for Publishing --- (Matches mqtt.PublishFunc type)
type PublishFunc func(topic, payload string) error

// --- Variable to hold the publisher callback ---
var mqttPublish PublishFunc // Renamed from mqttPublisher to avoid confusion

// --- Function to set the publisher callback ---
func SetMQTTPublisher(p PublishFunc) {
	log.Println("Bridge: Setting MQTT Publisher Callback...")
	mqttPublish = p
	if mqttPublish == nil {
		log.Println("Warning: MQTT Publisher callback set to nil in bridge.")
	}
}

// ConfigPayload struct definition for JSON parsing in ApplyConfigUpdate
type ConfigPayload struct {
	Debug     *bool   `json:"debug"`
	Direction *int    `json:"direction"`
	File      *string `json:"file"`
	Username  *string `json:"username"`
	SleepTime *int64  `json:"sleepTime"` // Microseconds
}

// --- Message Handlers ---

// handleCAN processes incoming CAN frames: converts and publishes to MQTT.
// Called internally by handleCANFrame in canbushandling.go.
func handleCAN(cf can.Frame) {
	if debugMode {
		log.Printf("ReceiveHandler: Processing CAN Frame: ID=%X Len=%d Data=%X", cf.ID, cf.Length, cf.Data[:cf.Length])
	}
	// Convert CAN frame to MQTT topic/payload
	mqttMsg := convert2MQTT(int(cf.ID), int(cf.Length), cf.Data) // Use the conversion function

	// Publish to MQTT if allowed by direction mode
	if directionMode != 2 { // Allow if bidirectional (0) or can2mqtt only (1)
		if mqttPublish == nil {
			log.Println("Error: Cannot publish CAN->MQTT message, MQTT Publisher callback not set.")
			return
		}
		err := mqttPublish(mqttMsg.Topic, mqttMsg.Payload) // Use the callback
		if err != nil {
			log.Printf("Error publishing CAN->MQTT (Topic: %s): %v", mqttMsg.Topic, err)
		} else {
			if debugMode {
				log.Printf("ReceiveHandler: Published CAN->MQTT: Topic=%s Payload=%s", mqttMsg.Topic, mqttMsg.Payload)
			}
			// Optional: Print summary to console
			// fmt.Printf("ID: %X Len: %d Data: %X -> Topic: \"%s\" Message: \"%s\"\n", cf.ID, cf.Length, cf.Data[:cf.Length], mqttMsg.Topic, mqttMsg.Payload)
		}
	} else {
		if debugMode {
			log.Printf("ReceiveHandler: dirMode=2 (mqtt2can only), MQTT message not published for CAN ID %X", cf.ID)
		}
	}
}

// HandleMessage handles standard incoming MQTT messages (non-config).
// It implements the mqtt.MessageHandler interface implicitly and is called by the MQTT client's default handler.
func HandleMessage(_ MQTT.Client, msg MQTT.Message) {
	topic := msg.Topic()
	payload := string(msg.Payload())

	if debugMode {
		log.Printf("ReceiveHandler: Processing MQTT Message: Topic=%s Payload=%s", topic, payload)
	}

	// Handle specific non-conversion translator commands if needed
	// HandleTranslator(topic, payload) // Call this if needed based on topic prefix etc.

	// Convert MQTT message to CAN frame
	cf := convert2CAN(topic, payload)

	// Publish CAN frame if conversion was successful (check for default/error frame?)
	// and if allowed by direction mode
	if cf.ID == 0xFFFFFFFF { // Check if conversion failed (using the error ID from convert2CAN)
		if debugMode {
			log.Printf("ReceiveHandler: Skipping CAN publish for topic %s due to conversion error.", topic)
		}
		return
	}

	if directionMode != 1 { // Allow if bidirectional (0) or mqtt2can only (2)
		canPublish(cf) // Publish to CAN bus (function defined in canbushandling.go)
		if debugMode {
			log.Printf(
				`ReceiveHandler: Published MQTT->CAN: ID=%X Len=%d Data=%X <- Topic: "%s" Message:%s \n`,
				cf.ID,
				cf.Length,
				cf.Data[:cf.Length],
				msg.Topic(),
				msg.Payload(),
			)
		}
		// Optional: Print summary to console
		// fmt.Printf("ID: %X Len: %d Data: %X <- Topic: \"%s\" Message: \"%s\"\n", cf.ID, cf.Length, cf.Data[:cf.Length], topic, payload)
	} else {
		if debugMode {
			log.Printf("ReceiveHandler: dirMode=1 (can2mqtt only), CAN frame not published for MQTT topic %s", topic)
		}
	}
}

// ApplyConfigUpdate handles incoming configuration messages from MQTT (translater/run topic).
// This function matches the mqtt.ConfigHandlerFunc type.
func ApplyConfigUpdate(payload string) {
	log.Printf("[ApplyConfigUpdate] Received config update payload: %s", payload)

	var cfgPayload ConfigPayload
	err := json.Unmarshal([]byte(payload), &cfgPayload)
	if err != nil {
		log.Printf("ApplyConfigUpdate Error: Failed to unmarshal JSON payload: %v", err)
		return // Stop processing if JSON is invalid
	}

	// Apply changes based on fields present in the JSON payload
	log.Println("Applying configuration changes...")
	if cfgPayload.Debug != nil {
		SetDbg(*cfgPayload.Debug)
	}
	if cfgPayload.Direction != nil {
		SetConfDirMode(strconv.Itoa(*cfgPayload.Direction))
	}
	if cfgPayload.File != nil {
		SetC2mf(*cfgPayload.File) // This setter now handles path expansion and triggers ReloadConfig
	}
	if cfgPayload.Username != nil {
		SetUserName(*cfgPayload.Username)
	}
	if cfgPayload.SleepTime != nil {
		// Convert int64 microseconds to string for the setter
		SetTimeSleepValue(strconv.FormatInt(*cfgPayload.SleepTime, 10))
	}
	log.Println("[ApplyConfigUpdate] Finished applying configuration changes.")
}

// HandleTranslator can be called if specific "translator/..." topics need handling
// that isn't standard conversion or config update.
func HandleTranslator(topic string, payload string) {
	if strings.HasPrefix(topic, "translater/command/") { // Example check
		log.Printf("Handling translator command: Topic=%s Payload=%s", topic, payload)
		// Add logic specific to translator commands here
	}
}

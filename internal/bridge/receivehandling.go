package bridge

import (
	"encoding/json"
	"fmt" // Added for potential error formatting
	"log"
	"strconv"
	"time" // Added for timing

	"github.com/brutella/can"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	// Import config only if needed for types directly, prefer passing data
	// config "github.com/farouk15160/Translater-code-new/internal/config"
)

// --- Callback Type for Publishing ---
type PublishFunc func(topic, payload string) error

// --- Variable to hold the publisher callback ---
var mqttPublish PublishFunc

// --- Function to set the publisher callback ---
func SetMQTTPublisher(p PublishFunc) {
	log.Println("Bridge: Setting MQTT Publisher Callback (for CAN->MQTT)...")
	mqttPublish = p
	if mqttPublish == nil {
		log.Println("Warning: MQTT Publisher callback (for CAN->MQTT) set to nil in bridge.")
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

// handleCAN processes incoming CAN frames concurrently.
// Called internally by handleCANFrame in canbushandling.go.
func handleCAN(cf can.Frame) {
	receivedTime := time.Now() // Record time when frame processing starts

	// *** Launch processing in a goroutine ***
	go func(frame can.Frame, rTime time.Time) {
		processingStartTime := time.Now()
		frameID := frame.ID & 0x1FFFFFFF // Mask ID for logging/lookup

		// Use the configured time sleep value, if any (apply per-message if needed)
		if timeSleepValue > 0 {
			time.Sleep(timeSleepValue)
		}

		if debugMode {
			log.Printf("ReceiveHandler-Go: Processing CAN Frame: ID=%X Len=%d Data=%X", frameID, frame.Length, frame.Data[:frame.Length])
		}

		// Convert CAN frame to MQTT topic/payload
		mqttMsg, convErr := convert2MQTT(int(frameID), int(frame.Length), frame.Data)
		if convErr != nil {
			if debugMode {
				log.Printf("ReceiveHandler-Go: Skipping MQTT publish for CAN ID %X due to conversion error: %v", frameID, convErr)
			}
			// Log processing time even if conversion failed
			log.Printf("[Perf] CAN->MQTT (ID: %X) processing time (Convert Error): %v", frameID, time.Since(processingStartTime))
			return // End goroutine
		}

		// Publish to MQTT (non-retained) if allowed by direction mode
		publishStartTime := time.Now()
		publishErr := fmt.Errorf("publish skipped due to direction mode") // Default error if skipped

		if directionMode != 2 { // Allow if bidirectional (0) or can2mqtt only (1)
			if mqttPublish == nil {
				publishErr = fmt.Errorf("cannot publish CAN->MQTT, MQTT Publisher callback not set")
				log.Println("Error:", publishErr)
			} else {
				// Use the standard (non-retained) Publish function
				publishErr = mqttPublish(mqttMsg.Topic, mqttMsg.Payload) // mqttPublish returns immediate error (nil usually)
				if publishErr != nil {
					// Error handled async in publisher, log here if needed
					log.Printf("Error initiating CAN->MQTT publish (Topic: %s): %v", mqttMsg.Topic, publishErr)
				} else {
					if debugMode {
						logPayload := mqttMsg.Payload
						if len(logPayload) > 100 {
							logPayload = logPayload[:100] + "..."
						}
						log.Printf("ReceiveHandler-Go: Initiated publish CAN->MQTT: Topic=%s Payload=%s", mqttMsg.Topic, logPayload)
					}
				}
			}
		} else {
			if debugMode {
				log.Printf("ReceiveHandler-Go: dirMode=2 (mqtt2can only), MQTT message not published for CAN ID %X", frameID)
			}
		}
		publishDuration := time.Since(publishStartTime)
		totalProcessingDuration := time.Since(processingStartTime)
		totalDurationSinceReception := time.Since(rTime)

		// Log performance timing
		if publishErr != nil && publishErr.Error() != "publish skipped due to direction mode" {
			log.Printf("[Perf] CAN->MQTT (ID: %X): Convert+Publish Error (%v) | Times: Convert=%v, PublishAttempt=%v, Total=%v (Since Recv: %v)",
				frameID, publishErr, publishStartTime.Sub(processingStartTime), publishDuration, totalProcessingDuration, totalDurationSinceReception)
		} else {
			log.Printf("[Perf] CAN->MQTT (ID: %X): Convert+Publish OK | Times: Convert=%v, Publish=%v, Total=%v (Since Recv: %v)",
				frameID, publishStartTime.Sub(processingStartTime), publishDuration, totalProcessingDuration, totalDurationSinceReception)
		}

	}(cf, receivedTime) // Pass frame by value to goroutine
}

// HandleMessage handles standard incoming MQTT messages concurrently.
// Called by the MQTT client's default handler.
func HandleMessage(_ MQTT.Client, msg MQTT.Message) {
	// This function is now called within a goroutine launched by mqtt_client's defaultHandler.
	// We can perform the work directly here.
	processingStartTime := time.Now() // Time when bridge handling starts
	topic := msg.Topic()
	// Payload is already a copy made by the defaultHandler
	payload := msg.Payload()

	if debugMode {
		logPayloadStr := string(payload)
		if len(logPayloadStr) > 100 {
			logPayloadStr = logPayloadStr[:100] + "..."
		}
		log.Printf("ReceiveHandler-Go: Processing MQTT Message: Topic=%s Payload=%s", topic, logPayloadStr)
	}

	// Convert MQTT message to CAN frame
	cf, convErr := convert2CAN(topic, payload) // Pass payload as []byte
	if convErr != nil {
		if debugMode {
			log.Printf("ReceiveHandler-Go: Skipping CAN publish for topic %s due to conversion error: %v", topic, convErr)
		}
		log.Printf("[Perf] MQTT->CAN (Topic: %s) processing time (Convert Error): %v", topic, time.Since(processingStartTime))
		return // End processing for this message
	}

	// Publish CAN frame if conversion was successful and allowed by direction mode
	publishStartTime := time.Now()
	publishErr := fmt.Errorf("publish skipped due to direction mode") // Default error if skipped

	if directionMode != 1 { // Allow if bidirectional (0) or mqtt2can only (2)
		publishErr = canPublish(cf) // canPublish now returns error
		if publishErr != nil {
			log.Printf("ReceiveHandler-Go: Error publishing MQTT->CAN: %v", publishErr)
		} else {
			if debugMode {
				log.Printf(
					`ReceiveHandler-Go: Published MQTT->CAN: ID=%X Len=%d Data=%X <- Topic: "%s"`,
					cf.ID,
					cf.Length,
					cf.Data[:cf.Length],
					topic,
				)
			}
		}
	} else {
		if debugMode {
			log.Printf("ReceiveHandler-Go: dirMode=1 (can2mqtt only), CAN frame not published for MQTT topic %s", topic)
		}
	}
	publishDuration := time.Since(publishStartTime)
	totalProcessingDuration := time.Since(processingStartTime)
	// Note: We don't have the original MQTT reception time here easily unless passed explicitly.

	// Log performance timing
	if publishErr != nil && publishErr.Error() != "publish skipped due to direction mode" {
		log.Printf("[Perf] MQTT->CAN (Topic: %s): Convert+Publish Error (%v) | Times: Convert=%v, PublishAttempt=%v, Total=%v",
			topic, publishErr, publishStartTime.Sub(processingStartTime), publishDuration, totalProcessingDuration)
	} else {
		log.Printf("[Perf] MQTT->CAN (Topic: %s): Convert+Publish OK | Times: Convert=%v, Publish=%v, Total=%v",
			topic, publishStartTime.Sub(processingStartTime), publishDuration, totalProcessingDuration)
	}
}

// ApplyConfigUpdate handles incoming configuration messages from MQTT ("translater/run" topic).
// This is called directly by the MQTT client's defaultHandler (in its goroutine).
func ApplyConfigUpdate(payload string) {
	log.Printf("[ApplyConfigUpdate@Bridge] Received config update payload: %s", payload)
	startTime := time.Now()

	var cfgPayload ConfigPayload
	err := json.Unmarshal([]byte(payload), &cfgPayload)
	if err != nil {
		log.Printf("ApplyConfigUpdate Error: Failed to unmarshal JSON payload: %v", err)
		return // Stop processing if JSON is invalid
	}

	// Apply changes by calling setters. The setters handle logging and potential reloads.
	log.Println("Applying configuration changes...")
	// **Consider protecting config changes with a mutex if reload takes time
	// or if conversions are happening concurrently.**
	// For now, assuming setters are quick or ReloadConfig handles locking.
	if cfgPayload.Debug != nil {
		SetDbg(*cfgPayload.Debug)
	}
	if cfgPayload.Direction != nil {
		SetConfDirMode(strconv.Itoa(*cfgPayload.Direction))
	}
	if cfgPayload.File != nil {
		SetC2mf(*cfgPayload.File) // This setter triggers ReloadConfig
	}
	if cfgPayload.Username != nil {
		SetUserName(*cfgPayload.Username)
	}
	if cfgPayload.SleepTime != nil {
		SetTimeSleepValue(strconv.FormatInt(*cfgPayload.SleepTime, 10))
	}
	log.Printf("[ApplyConfigUpdate@Bridge] Finished applying configuration changes in %v.", time.Since(startTime))
}

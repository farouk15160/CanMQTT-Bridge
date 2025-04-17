package bridge

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv" // Import strconv
	"sync"
	"time"

	"github.com/brutella/can"
	MQTT "github.com/eclipse/paho.mqtt.golang"
)

// Define ConfigPayload struct at package level
// Used for unmarshalling MQTT messages on translater/run topic
type ConfigPayload struct {
	Debug     *bool   `json:"debug"`
	Direction *int    `json:"direction"`
	File      *string `json:"file"`
	Username  *string `json:"username"`
	SleepTime *int64  `json:"sleepTime"` // Microseconds
	BitSize   *int    `json:"bit_size"`  // ADDED
}

// --- Callback Type for Publishing ---
// Defined in interfaces.go: type Publisher interface { ... }

// --- Variable to hold the publisher callback ---
// Defined in main.go: var mqttPublisher Publisher

// --- Function to set the publisher callback ---
func SetMQTTPublisher(p Publisher) { // Changed to use Publisher interface
	log.Println("Bridge: Setting MQTT Publisher Callback (for CAN->MQTT)...")
	ConfigLock.Lock() // Use exported name - Protect global state assignment
	mqttPublisher = p
	ConfigLock.Unlock() // Use exported name
	if p == nil {
		log.Println("Warning: MQTT Publisher callback (for CAN->MQTT) set to nil in bridge.")
	}
}

// ApplyConfigUpdate handles incoming configuration messages from MQTT ("translater/run" topic).
// Called directly by the MQTT client's defaultHandler (in its goroutine).
func ApplyConfigUpdate(payload string) {
	log.Printf("[ApplyConfigUpdate@Bridge] Received config update payload: %s", payload)
	startTime := time.Now()

	var cfgPayload ConfigPayload // Uses the package-level definition now

	err := json.Unmarshal([]byte(payload), &cfgPayload)
	if err != nil {
		log.Printf("ApplyConfigUpdate Error: Failed to unmarshal JSON payload: %v", err)
		return // Stop processing if JSON is invalid
	}

	// Apply changes by calling setters from main.go.
	log.Println("Applying configuration changes...")
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
	// --- ADDED: Handle BitSize ---
	if cfgPayload.BitSize != nil {
		SetBitSize(*cfgPayload.BitSize) // Call the new setter
	}
	// --- END ADDED ---
	log.Printf("[ApplyConfigUpdate@Bridge] Finished applying configuration changes in %v.", time.Since(startTime))
}

// --- Worker Functions ---

// canProcessor is run by worker goroutines to process CAN frames.
func canProcessor(workerID int, wg *sync.WaitGroup) {
	defer wg.Done()
	log.Printf("CAN Worker %d: Started", workerID)

	for {
		select {
		case <-stopChan:
			log.Printf("CAN Worker %d: Stopping...", workerID)
			return
		case frame, ok := <-canWorkChan:
			if !ok {
				log.Printf("CAN Worker %d: Work channel closed, stopping.", workerID)
				return // Channel closed
			}
			processCANFrame(frame, workerID)
		}
	}
}

// mqttProcessor is run by worker goroutines to process MQTT messages.
func mqttProcessor(workerID int, wg *sync.WaitGroup) {
	defer wg.Done()
	log.Printf("MQTT Worker %d: Started", workerID)

	for {
		select {
		case <-stopChan:
			log.Printf("MQTT Worker %d: Stopping...", workerID)
			return
		case item, ok := <-mqttWorkChan:
			if !ok {
				log.Printf("MQTT Worker %d: Work channel closed, stopping.", workerID)
				return // Channel closed
			}
			processMQTTMessage(item, workerID)
		}
	}
}

// --- Processing Logic (Called by Workers) ---

// processCANFrame converts a CAN frame and publishes it via MQTT.
func processCANFrame(frame can.Frame, workerID int) {
	processingStartTime := time.Now()
	frameID := frame.ID & 0x1FFFFFFF // Mask ID

	// Local access to config values for less locking contention within the loop
	ConfigLock.RLock() // Use exported name - Read lock
	currentDebugMode := debugMode
	currentDirectionMode := directionMode
	currentTimeSleepValue := timeSleepValue
	currentPublisher := mqttPublisher
	ConfigLock.RUnlock() // Use exported name - Release lock

	if currentTimeSleepValue > 0 {
		time.Sleep(currentTimeSleepValue)
	}

	if currentDebugMode {
		log.Printf("CAN Worker %d: Processing CAN Frame: ID=%X Len=%d Data=%X", workerID, frameID, frame.Length, frame.Data[:frame.Length])
	}

	// Convert CAN frame using the exported function
	mqttMsg, convRule, convErr := Convert2MQTTUsingMap(frameID, int(frame.Length), frame.Data) // Use exported name
	if convErr != nil {
		if currentDebugMode && convErr.Error() != "no matching conversion rule found" { // Reduce log spam for non-matching rules
			log.Printf("CAN Worker %d: Skipping MQTT publish for CAN ID %X due to conversion error: %v", workerID, frameID, convErr)
		}
		// Log processing time even if conversion failed
		// log.Printf("[Perf] CAN->MQTT (ID: %X, Worker: %d) Convert Error: %v", frameID, workerID, time.Since(processingStartTime)) // Maybe too verbose
		return // End processing
	}
	if convRule == nil { // Should not happen if err is nil, but check defensively
		log.Printf("CAN Worker %d: Error - nil conversion rule returned for ID %X", workerID, frameID)
		return
	}

	// Publish to MQTT if allowed
	publishStartTime := time.Now()
	publishErr := fmt.Errorf("publish skipped due to direction mode")

	if currentDirectionMode != 2 { // Allow if bidirectional (0) or can2mqtt only (1)
		if currentPublisher == nil {
			publishErr = fmt.Errorf("cannot publish CAN->MQTT, MQTT Publisher not set")
			log.Printf("CAN Worker %d Error: %v", workerID, publishErr)
		} else {
			publishErr = currentPublisher.Publish(mqttMsg.Topic, mqttMsg.Payload) // Use non-retained
			if publishErr != nil {
				// Error is logged async by publisher usually, log context here if needed
				log.Printf("CAN Worker %d: Error initiating CAN->MQTT publish (Topic: %s): %v", workerID, mqttMsg.Topic, publishErr)
			} else if currentDebugMode {
				// Log truncated payload...
				logPayload := mqttMsg.Payload
				if len(logPayload) > 100 {
					logPayload = logPayload[:100] + "..."
				}
				log.Printf("CAN Worker %d: Initiated publish CAN->MQTT: Topic=%s Payload=%s", workerID, mqttMsg.Topic, logPayload)
			}
		}
	} else if currentDebugMode {
		// log.Printf("CAN Worker %d: dirMode=2, MQTT message not published for CAN ID %X", workerID, frameID) // Reduce noise
	}
	publishDuration := time.Since(publishStartTime)
	totalProcessingDuration := time.Since(processingStartTime)

	// Log performance
	if publishErr != nil && publishErr.Error() != "publish skipped due to direction mode" {
		log.Printf("[Perf] CAN->MQTT (ID: %X, Worker: %d): Convert+Publish Error (%v) | Times: Convert=%v, PublishAttempt=%v, Total=%v",
			frameID, workerID, publishErr, publishStartTime.Sub(processingStartTime), publishDuration, totalProcessingDuration)
	} else {
		log.Printf("[Perf] CAN->MQTT (ID: %X, Worker: %d): Convert+Publish OK | Times: Convert=%v, Publish=%v, Total=%v",
			frameID, workerID, publishStartTime.Sub(processingStartTime), publishDuration, totalProcessingDuration)
	}
}

// processMQTTMessage converts an MQTT message and publishes it via CAN.
func processMQTTMessage(item MqttWorkItem, workerID int) {
	processingStartTime := time.Now()
	topic := item.Topic
	payload := item.Payload

	ConfigLock.RLock() // Use exported name - Read lock
	currentDebugMode := debugMode
	currentDirectionMode := directionMode
	ConfigLock.RUnlock() // Use exported name - Release lock

	if currentDebugMode {
		logPayloadStr := string(payload)
		if len(logPayloadStr) > 100 {
			logPayloadStr = logPayloadStr[:100] + "..."
		}
		log.Printf("MQTT Worker %d: Processing MQTT Message: Topic=%s Payload=%s", workerID, topic, logPayloadStr)
	}

	// Convert MQTT message to CAN frame using the exported function
	cf, convRule, convErr := Convert2CANUsingMap(topic, payload) // Use exported name
	if convErr != nil {
		if currentDebugMode && convErr.Error() != "no matching conversion rule found" {
			log.Printf("MQTT Worker %d: Skipping CAN publish for topic %s due to conversion error: %v", workerID, topic, convErr)
		}
		// log.Printf("[Perf] MQTT->CAN (Topic: %s, Worker: %d) Convert Error: %v", topic, workerID, time.Since(processingStartTime)) // Maybe too verbose
		return
	}
	if convRule == nil {
		log.Printf("MQTT Worker %d: Error - nil conversion rule returned for Topic %s", workerID, topic)
		return
	}

	// Publish CAN frame if allowed
	publishStartTime := time.Now()
	publishErr := fmt.Errorf("publish skipped due to direction mode")

	if currentDirectionMode != 1 { // Allow if bidirectional (0) or mqtt2can only (2)
		publishErr = canPublish(cf) // canPublish logs its own errors internally now
		if publishErr != nil {
			// Error already logged by canPublish
		} else if currentDebugMode {
			log.Printf(
				`MQTT Worker %d: Published MQTT->CAN: ID=%X Len=%d Data=%X <- Topic: "%s"`,
				workerID, cf.ID, cf.Length, cf.Data[:cf.Length], topic,
			)
		}
	} else if currentDebugMode {
		// log.Printf("MQTT Worker %d: dirMode=1, CAN frame not published for MQTT topic %s", workerID, topic) // Reduce noise
	}
	publishDuration := time.Since(publishStartTime)
	totalProcessingDuration := time.Since(processingStartTime)

	// Log performance
	if publishErr != nil && publishErr.Error() != "publish skipped due to direction mode" {
		log.Printf("[Perf] MQTT->CAN (Topic: %s, Worker: %d): Convert+Publish Error (%v) | Times: Convert=%v, PublishAttempt=%v, Total=%v",
			topic, workerID, publishErr, publishStartTime.Sub(processingStartTime), publishDuration, totalProcessingDuration)
	} else {
		log.Printf("[Perf] MQTT->CAN (Topic: %s, Worker: %d): Convert+Publish OK | Times: Convert=%v, Publish=%v, Total=%v",
			topic, workerID, publishStartTime.Sub(processingStartTime), publishDuration, totalProcessingDuration)
	}
}

// HandleMessage dispatches incoming MQTT messages to the worker pool.
// Called by the MQTT client's default handler (which runs in its own goroutine).
func HandleMessage(_ MQTT.Client, msg MQTT.Message) {
	// Copy topic and payload immediately
	topic := msg.Topic()
	payloadCopy := make([]byte, len(msg.Payload()))
	copy(payloadCopy, msg.Payload())

	item := MqttWorkItem{
		Topic:   topic,
		Payload: payloadCopy,
	}

	// Try sending to the channel, handle potential blocking or closed channel
	select {
	case mqttWorkChan <- item:
		// Successfully sent to worker channel
	default:
		// Channel might be full or closed
		ConfigLock.RLock() // Use exported name - Safely read debug mode
		dbg := debugMode
		ConfigLock.RUnlock() // Use exported name
		if dbg {             // Log only if debugging, as this indicates potential overload
			log.Printf("Warning: MQTT work channel full or closed. Discarding message for topic: %s", topic)
		}
		// Optional: Add metrics here to track discarded messages
	}
}

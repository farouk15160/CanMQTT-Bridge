package bridge

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/brutella/can"
	MQTT "github.com/eclipse/paho.mqtt.golang"
)

type ConfigPayload struct {
	Debug     *bool   `json:"debug"`
	Direction *int    `json:"direction"`
	File      *string `json:"file"`
	Username  *string `json:"username"`
	SleepTime *int64  `json:"sleepTime"` // Microseconds
	BitSize   *int    `json:"bit_size"`  // ADDED
}

func SetMQTTPublisher(p Publisher) { 
	log.Println("Bridge: Setting MQTT Publisher Callback (for CAN->MQTT)...")
	ConfigLock.Lock() 
	mqttPublisher = p
	ConfigLock.Unlock() 
	if p == nil {
		log.Println("Warning: MQTT Publisher callback (for CAN->MQTT) set to nil in bridge.")
	}
}

func ApplyConfigUpdate(payload string) {
	log.Printf("[ApplyConfigUpdate@Bridge] Received config update payload: %s", payload)
	startTime := time.Now()

	var cfgPayload ConfigPayload // 

	err := json.Unmarshal([]byte(payload), &cfgPayload)
	if err != nil {
		log.Printf("ApplyConfigUpdate Error: Failed to unmarshal JSON payload: %v", err)
		return 
	}

	log.Println("Applying configuration changes...")
	if cfgPayload.Debug != nil {
		SetDbg(*cfgPayload.Debug)
	}
	if cfgPayload.Direction != nil {
		SetConfDirMode(strconv.Itoa(*cfgPayload.Direction))
	}
	if cfgPayload.File != nil {
		SetC2mf(*cfgPayload.File) 
	}
	if cfgPayload.Username != nil {
		SetUserName(*cfgPayload.Username)
	}
	if cfgPayload.SleepTime != nil {
		SetTimeSleepValue(strconv.FormatInt(*cfgPayload.SleepTime, 10))
	}
	// 
	if cfgPayload.BitSize != nil {
		SetBitSize(*cfgPayload.BitSize) 
	}
	log.Printf("[ApplyConfigUpdate@Bridge] Finished applying configuration changes in %v.", time.Since(startTime))
}


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
				return 
			}
			processCANFrame(frame, workerID)
		}
	}
}

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
				return 
			}
			processMQTTMessage(item, workerID)
		}
	}
}

func processCANFrame(frame can.Frame, workerID int) {
	processingStartTime := time.Now()
	frameID := frame.ID & 0x1FFFFFFF 
	ConfigLock.RLock() 
	currentDebugMode := debugMode
	currentDirectionMode := directionMode
	currentTimeSleepValue := timeSleepValue
	currentPublisher := mqttPublisher
	ConfigLock.RUnlock() 

	if currentTimeSleepValue > 0 {
		time.Sleep(currentTimeSleepValue)
	}

	if currentDebugMode {
		log.Printf("CAN Worker %d: Processing CAN Frame: ID=%X Len=%d Data=%X", workerID, frameID, frame.Length, frame.Data[:frame.Length])
	}

	mqttMsg, convRule, convErr := Convert2MQTTUsingMap(frameID, int(frame.Length), frame.Data) 
	if convErr != nil {
		if currentDebugMode && convErr.Error() != "no matching conversion rule found" { 
			log.Printf("CAN Worker %d: Skipping MQTT publish for CAN ID %X due to conversion error: %v", workerID, frameID, convErr)
		}
		return 
	}
	if convRule == nil { 
		log.Printf("CAN Worker %d: Error - nil conversion rule returned for ID %X", workerID, frameID)
		return
	}
	publishStartTime := time.Now()
	publishErr := fmt.Errorf("publish skipped due to direction mode")

	if currentDirectionMode != 2 { 
		if currentPublisher == nil {
			publishErr = fmt.Errorf("cannot publish CAN->MQTT, MQTT Publisher not set")
			log.Printf("CAN Worker %d Error: %v", workerID, publishErr)
		} else {
			publishErr = currentPublisher.Publish(mqttMsg.Topic, mqttMsg.Payload) 
			if publishErr != nil {
				log.Printf("CAN Worker %d: Error initiating CAN->MQTT publish (Topic: %s): %v", workerID, mqttMsg.Topic, publishErr)
			} else if currentDebugMode {
				logPayload := mqttMsg.Payload
				if len(logPayload) > 100 {
					logPayload = logPayload[:100] + "..."
				}
				log.Printf("CAN Worker %d: Initiated publish CAN->MQTT: Topic=%s Payload=%s", workerID, mqttMsg.Topic, logPayload)
			}
		}
	} else if currentDebugMode {
		log.Printf("CAN Worker %d: dirMode=2, MQTT message not published for CAN ID %X", workerID, frameID) // Reduce noise
	}
	publishDuration := time.Since(publishStartTime)
	totalProcessingDuration := time.Since(processingStartTime)
	if publishErr != nil && publishErr.Error() != "publish skipped due to direction mode" {
		log.Printf("[Perf] CAN->MQTT (ID: %X, Worker: %d): Convert+Publish Error (%v) | Times: Convert=%v, PublishAttempt=%v, Total=%v",
			frameID, workerID, publishErr, publishStartTime.Sub(processingStartTime), publishDuration, totalProcessingDuration)
	} else {
		log.Printf("[Perf] CAN->MQTT (ID: %X, Worker: %d): Convert+Publish OK | Times: Convert=%v, Publish=%v, Total=%v",
			frameID, workerID, publishStartTime.Sub(processingStartTime), publishDuration, totalProcessingDuration)
	}
}

func processMQTTMessage(item MqttWorkItem, workerID int) {
	processingStartTime := time.Now()
	topic := item.Topic
	payload := item.Payload
	ConfigLock.RLock() 
	currentDebugMode := debugMode
	currentDirectionMode := directionMode
	ConfigLock.RUnlock() 

	if currentDebugMode {
		logPayloadStr := string(payload)
		if len(logPayloadStr) > 100 {
			logPayloadStr = logPayloadStr[:100] + "..."
		}
		log.Printf("MQTT Worker %d: Processing MQTT Message: Topic=%s Payload=%s", workerID, topic, logPayloadStr)
	}

	cf, convRule, convErr := Convert2CANUsingMap(topic, payload) 
	if convErr != nil {
		if currentDebugMode && convErr.Error() != "no matching conversion rule found" {
			log.Printf("MQTT Worker %d: Skipping CAN publish for topic %s due to conversion error: %v", workerID, topic, convErr)
		}
		log.Printf("[Perf] MQTT->CAN (Topic: %s, Worker: %d) Convert Error: %v", topic, workerID, time.Since(processingStartTime)) // Maybe too verbose
		return
	}
	if convRule == nil {
		log.Printf("MQTT Worker %d: Error - nil conversion rule returned for Topic %s", workerID, topic)
		return
	}

	publishStartTime := time.Now()
	publishErr := fmt.Errorf("publish skipped due to direction mode")

	if currentDirectionMode != 1 {
		publishErr = canPublish(cf) 
		if publishErr != nil {
		} else if currentDebugMode {
			log.Printf(
				`MQTT Worker %d: Published MQTT->CAN: ID=%X Len=%d Data=%X <- Topic: "%s"`,
				workerID, cf.ID, cf.Length, cf.Data[:cf.Length], topic,
			)
		}
	} else if currentDebugMode {
		log.Printf("MQTT Worker %d: dirMode=1, CAN frame not published for MQTT topic %s", workerID, topic) // Reduce noise
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

func HandleMessage(_ MQTT.Client, msg MQTT.Message) {
	topic := msg.Topic()
	payloadCopy := make([]byte, len(msg.Payload()))
	copy(payloadCopy, msg.Payload())

	item := MqttWorkItem{
		Topic:   topic,
		Payload: payloadCopy,
	}
	select {
	case mqttWorkChan <- item:
	default:
		ConfigLock.RLock() 
		dbg := debugMode
		ConfigLock.RUnlock() 
		if dbg {             
			log.Printf("Warning: MQTT work channel full or closed. Discarding message for topic: %s", topic)
		}
	}
}

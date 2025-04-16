package mqtt

import (
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"strconv"
	"time" // Added for timestamp

	bridge "github.com/farouk15160/Translater-code-new/internal/bridge"
	config "github.com/farouk15160/Translater-code-new/internal/config"
)

// handleConfigUpdate processes the JSON payload from the "translater/run" topic.
// It calls the appropriate Setters in the bridge package.
func handleConfigUpdate(payload string) {
	log.Printf("[handleConfigUpdate] Received config update on 'translater/run': %s", payload)

	var cfgPayload ConfigPayload
	err := json.Unmarshal([]byte(payload), &cfgPayload)
	if err != nil {
		log.Printf("Error unmarshalling JSON payload for config update: %v", err)
		return // Stop processing if JSON is invalid
	}

	// Apply changes based on fields present in the JSON
	log.Println("Applying configuration changes...")
	if cfgPayload.Debug != nil {
		bridge.SetDbg(*cfgPayload.Debug) // Directly pass the bool value
	}
	if cfgPayload.Direction != nil {
		dirStr := strconv.Itoa(*cfgPayload.Direction)
		bridge.SetConfDirMode(dirStr)
	}
	if cfgPayload.File != nil {
		bridge.SetC2mf(*cfgPayload.File) // Sets the variable AND triggers reload via bridge logic
	}
	if cfgPayload.Username != nil {
		bridge.SetUserName(*cfgPayload.Username) // Directly pass the string value
	}
	if cfgPayload.SleepTime != nil {
		sleepStr := strconv.FormatInt(*cfgPayload.SleepTime, 10)
		bridge.SetTimeSleepValue(sleepStr)
	}
	log.Println("[handleConfigUpdate] Finished processing config update.")
}

// handleTranslatorStatus gathers system information and publishes it to "translater/status".
// It is triggered by receiving a message on "translater/process".
func handleTranslatorStatus(requestTopic string, payload string, mqttClient *Client) {
	log.Printf("[handleTranslatorStatus] Received request on topic '%s', gathering status...", requestTopic)
	_ = payload             // Payload of request message is currently ignored
	startTime := time.Now() // Start timing the handler itself

	var status TranslatorStatus
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	status.RAMUsage = m.Alloc             // RAM allocated by heap objects
	status.BufferUsage = getBufferUsage() // Get buffer usage (needs custom implementation)
	status.CPUUsageCores = getCPUUsage()  // Get CPU usage (overall in this implementation)
	status.Temperature = getTemperature() // Get system temperature
	status.Uptime = getUptime()           // Get system uptime
	gatherTime := time.Now()              // Time after gathering stats

	// --- Convert status to human-readable JSON ---
	readableStatus := ReadableTranslatorStatus{
		BufferUsage:   status.BufferUsage,
		CPUUsageCores: make(map[string]string),
		Temperature:   fmt.Sprintf("%.1f°C", status.Temperature),
		Uptime:        formatUptime(status.Uptime),
	}
	if status.Temperature < 0 {
		readableStatus.Temperature = "N/A"
	}

	totalMemory := getTotalMemory()
	if totalMemory > 0 {
		ramUsagePercent := (float64(status.RAMUsage) / float64(totalMemory)) * 100
		readableStatus.RAMUsage = fmt.Sprintf("%.2f%% (%d MB)", ramUsagePercent, status.RAMUsage/(1024*1024))
	} else {
		readableStatus.RAMUsage = fmt.Sprintf("%d MB (Total unknown)", status.RAMUsage/(1024*1024))
	}

	if len(status.CPUUsageCores) > 0 {
		readableStatus.CPUUsageCores["overall"] = fmt.Sprintf("%.2f%%", status.CPUUsageCores[0]*100)
	} else {
		readableStatus.CPUUsageCores["overall"] = "N/A"
	}

	// *** Use json.Marshal instead of json.MarshalIndent for slight speed increase ***
	jsonBytes, err := json.Marshal(readableStatus) // Changed here
	if err != nil {
		log.Printf("Error marshalling readable status to JSON: %v", err)
		return
	}
	payloadString := string(jsonBytes)
	marshalTime := time.Now() // Time after marshalling

	// --- Publish the status to "translater/status" with retain flag ---
	statusTopic := "translater/status"
	if err := mqttClient.PublishRetained(statusTopic, payloadString); err != nil {
		// Error handled async in publisher
	} else {
		log.Printf("Initiated retained status update publish to '%s'", statusTopic)
	}
	publishInitiateTime := time.Now() // Time after initiating publish

	// Log detailed timing if needed
	if mqttClient.debug { // Only log detailed perf if debug is on
		log.Printf("[Perf Detail] Status Handler Timing: Gather=%v, Marshal=%v, PublishInit=%v, Total=%v",
			gatherTime.Sub(startTime),
			marshalTime.Sub(gatherTime),
			publishInitiateTime.Sub(marshalTime),
			time.Since(startTime))
	}
}

// PublishStartInfo publishes a retained message indicating the translator is running.
func PublishStartInfo(c *Client) { // Expects *mqtt.Client
	startTopic := "translater/start"
	startMessage := "CAN-MQTT Translator is up and running"
	ip := getIPAddress()

	uname := "unknown"
	if config.UsernameFlag != nil && *config.UsernameFlag != "" {
		uname = *config.UsernameFlag
	}

	payloadMap := map[string]string{
		"message":    startMessage,
		"ip_address": ip,
		"username":   uname,
		"timestamp":  strconv.FormatInt(time.Now().Unix(), 10),
	}

	// Use MarshalIndent here as it only happens once on startup
	jsonBytes, err := json.MarshalIndent(payloadMap, "", "  ")
	if err != nil {
		log.Printf("Error marshaling start info JSON: %v", err)
		return
	}
	jsonPayload := string(jsonBytes)

	if err := c.PublishRetained(startTopic, jsonPayload); err != nil {
		// Error handled async in publisher
	} else {
		log.Printf("Initiated retained start info publish to '%s'", startTopic)
	}
}

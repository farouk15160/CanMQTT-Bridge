package mqtt

import (
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"strconv"

	bridge "github.com/farouk15160/Translater-code-new/internal/bridge"
	config "github.com/farouk15160/Translater-code-new/internal/config"
)

// handleConfigUpdate processes the JSON payload from the "translater/run" topic.
func handleConfigUpdate(payload string) {
	log.Printf("[handleConfigUpdate] Received config update on 'translater/run': %s", payload)

	var cfgPayload ConfigPayload
	err := json.Unmarshal([]byte(payload), &cfgPayload)
	if err != nil {
		log.Printf("Error unmarshalling JSON payload for config update: %v", err)
		return // Stop processing if JSON is invalid
	}

	// Apply changes based on fields present in the JSON
	if cfgPayload.Debug != nil {
		bridge.SetDbg(*cfgPayload.Debug) // Directly pass the bool value
		log.Printf("Config Update: Set debug mode to: %t", *cfgPayload.Debug)
	}

	if cfgPayload.Direction != nil {
		// Convert the int direction to string for SetConfDirMode
		dirStr := strconv.Itoa(*cfgPayload.Direction)
		bridge.SetConfDirMode(dirStr) // Pass the direction value as string "0", "1", or "2"
		// SetConfDirMode already logs the change and validates the value
	}

	if cfgPayload.File != nil {
		// Warning: Runtime change needs ReloadConfig logic in bridge package
		log.Printf("Config Update: Request to change config file to '%s'. Runtime reload NOT implemented.", *cfgPayload.File)
		bridge.SetC2mf(*cfgPayload.File) // Sets the variable, but doesn't reload yet
		// bridge.ReloadConfig() // Uncomment if reload logic is implemented in bridge
	}

	if cfgPayload.Username != nil {
		bridge.SetUserName(*cfgPayload.Username) // Directly pass the string value
		// SetUserName already logs the change
	}

	if cfgPayload.SleepTime != nil {
		// Convert the int64 sleep time (microseconds) to string for SetTimeSleepValue
		sleepStr := strconv.FormatInt(*cfgPayload.SleepTime, 10)
		bridge.SetTimeSleepValue(sleepStr) // Pass microseconds as string
		// SetTimeSleepValue already logs the change and parses the string
	}

	log.Println("[handleConfigUpdate] Finished processing config update.")
}

// handleTranslatorStatus gathers system information and publishes it.
func handleTranslatorStatus(topic string, payload string, mqttClient *Client) { // Receive *Client
	log.Printf("[handleTranslatorStatus] Received request for status on topic: %s", topic)
	_ = payload

	var status TranslatorStatus

	// --- 1. Get RAM Usage ---
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	status.RAMUsage = m.Alloc

	// --- 2. Get Buffer Usage ---
	// This depends entirely on how you buffer data in your application.
	// You'll need to replace this with your actual buffer usage logic.
	status.BufferUsage = getBufferUsage() // Implement this function

	// --- 3. Get CPU Usage ---
	// Getting precise CPU usage per core is OS-specific and complex in Go.
	// This is a simplified example; you might need to use external libraries
	// or OS-specific methods for accurate CPU monitoring.
	status.CPUUsageCores = getCPUUsage() // Implement this function

	// --- 4. Get Temperature ---
	// Getting system temperature is OS-specific. Go doesn't provide
	// a standard library for this. You might need external tools or libraries.
	status.Temperature = getTemperature() // Implement this function

	// --- 5. Get Uptime ---
	// Uptime is also OS-specific. This is a placeholder.
	status.Uptime = getUptime() // Implement this function

	// --- Convert status to JSON ---
	readableStatus := ReadableTranslatorStatus{
		BufferUsage:   status.BufferUsage,
		CPUUsageCores: make(map[string]string),
		Temperature:   fmt.Sprintf("%.1f °C", status.Temperature), // Format with unit
		Uptime:        formatUptime(status.Uptime),                // Format uptime
	}

	// Calculate RAM usage percentage
	var totalMemory uint64 // You'll need to implement a function to get total system memory
	totalMemory = getTotalMemory()
	ramUsagePercent := (float64(status.RAMUsage) / float64(totalMemory)) * 100
	readableStatus.RAMUsage = fmt.Sprintf("%.2f%%", ramUsagePercent)

	// Populate CPU usage with core labels and format as percentage
	for i, usage := range status.CPUUsageCores {
		readableStatus.CPUUsageCores[fmt.Sprintf("core_%d", i+1)] = fmt.Sprintf("%.2f%%", usage*100)
	}

	// Convert the readable status to JSON
	jsonBytes, err := json.Marshal(readableStatus)
	if err != nil {
		log.Printf("Error marshalling status to JSON: %v", err)
		return
	}

	// Convert the byte slice to a string
	payloadString := string(jsonBytes)

	// Use the retained publish function from the client
	// MQTT Publish
	if err := mqttClient.PublishRetained(topic, payloadString); err != nil { // Changed topic slightly
		log.Printf("Error publishing retained status to %v: %v", topic, err)
	} else {

		log.Printf("MQTT Publish -> Topic=%s | Payload=%s", topic, payloadString)
	}

}

// PublishStartInfo publishes a retained message indicating the translator is running.
func PublishStartInfo(c *Client) { // Expects *mqtt.Client
	startMessage := "CAN-MQTT Translator is up and running"
	ip := getIPAddress() // Get local IP

	payloadMap := map[string]string{
		"message":    startMessage,
		"ip_address": ip,
		"username":   *config.UsernameFlag,
	}

	jsonBytes, err := json.Marshal(payloadMap)
	if err != nil {
		log.Printf("Error marshaling start info JSON: %v", err)
		return
	}
	jsonPayload := string(jsonBytes)

	// Use the retained publish function from the client
	if err := c.PublishRetained("translater/start", jsonPayload); err != nil { // Changed topic slightly
		log.Printf("Error publishing retained status to 'translater/start': %v", err)
	} else {
		log.Printf("Published retained message to 'translater/start': %s", jsonPayload)
	}
}

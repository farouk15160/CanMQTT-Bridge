package mqtt

import (
	"encoding/json" // Added for JSON parsing
	"fmt"
	"log"
	"strconv" // Added for converting numbers to string for bridge functions

	// Import the bridge package to access Set... functions
	"github.com/farouk15160/Translater-code-new/internal/bridge"
)

// ConfigPayload struct mirrors the expected JSON structure.
// Using pointers allows us to check if a field was present in the JSON.
type ConfigPayload struct {
	Debug     *bool   `json:"debug"`
	Direction *int    `json:"direction"` // Expecting 0, 1, or 2
	File      *string `json:"file"`
	Username  *string `json:"username"`
	SleepTime *int64  `json:"sleepTime"` // Using int64 for flexibility, represents microseconds
}

// dispatchMessage routes incoming MQTT messages.
func dispatchMessage(topic, payload string) {
	// Route the consolidated config message
	if topic == "translater/run" {
		handleConfigUpdate(payload)
		return
	}

	// Add other specific topic routing rules if needed...
	// if strings.HasPrefix(topic, "some/prefix") {
	// 	handlePrefixMessage(topic, payload)
	// 	return
	// }

	// Messages not handled above will implicitly be passed to the
	// bridge's handleMQTT function via the default handler in mqtt_client.go
	// (assuming defaultHandler calls bridge.handleMQTT)
	// log.Printf("[dispatchMessage] Topic '%s' not specifically routed, passing to default handler.", topic)
}

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

// --- Keep or remove other handlers as needed ---

// handlePrefixMessage is an example function for topics like "some/prefix/..."
func handlePrefixMessage(topic, payload string) {
	fmt.Printf("[handlePrefixMessage] topic=%s payload=%s\n", topic, payload)
	// ... your custom logic here ...
}

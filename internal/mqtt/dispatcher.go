package mqtt

// ConfigPayload struct mirrors the expected JSON structure.
// Using pointers allows us to check if a field was present in the JSON.
type ConfigPayload struct {
	Debug     *bool   `json:"debug"`
	Direction *int    `json:"direction"` // Expecting 0, 1, or 2
	File      *string `json:"file"`
	Username  *string `json:"username"`
	SleepTime *int64  `json:"sleepTime"` // Using int64 for flexibility, represents microseconds
}

// // dispatchMessage routes incoming MQTT messages.
// func dispatchMessage(topic, payload string, mqttClient *Client) bool {
// 	// Handle "translater/run" for config updates
// 	if topic == "translater/run" {
// 		handleConfigUpdate(payload)
// 		return true // Message handled
// 	}
// 	if topic == "translater/process" {
// 		handleTranslatorStatus(topic, payload, mqttClient)
// 		return true // Message handled
// 	}

// 	// If the topic is not handled specifically, let the default handler process it.
// 	return false // Message NOT handled
// }

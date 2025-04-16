package bridge

// Publisher defines the MQTT publishing capabilities needed by the bridge.
// This should match the methods provided by the actual MQTT client implementation.
type Publisher interface {
	Publish(topic, payload string) error         // For non-retained messages (e.g., CAN->MQTT data)
	PublishRetained(topic, payload string) error // For retained messages (less common for bridge to call directly)
	// Add other methods like IsConnected() if the bridge logic needs them.
}

// internal/bridge/interfaces.go
package bridge

// Publisher defines the MQTT publishing capabilities needed by the bridge.
type Publisher interface {
	Publish(topic, payload string) error
	PublishRetained(topic, payload string) error
	// Add IsConnected() bool if needed later
}

// Note: Configurator interface is removed as we use a direct function callback (`ConfigHandlerFunc`)

// internal/mqtt/interfaces.go
package mqtt

import MQTT "github.com/eclipse/paho.mqtt.golang"

// MessageHandler defines the standard message handling capability needed by the MQTT client
// for messages that are NOT configuration updates.
type MessageHandler interface {
	HandleMessage(client MQTT.Client, msg MQTT.Message)
}
type ClockConfigPayload struct {
	Takt *uint8 `json:"takt"` // Pointer to check if the key was present
}

// Note: ConfigHandlerFunc type is defined directly in mqtt_client.go now.

type TranslatorStatus struct {
	RAMUsage      uint64      `json:"ram_usage"`       // In bytes
	BufferUsage   BufferUsage `json:"buffer_usage"`    // Example: Percentage or queue length
	CPUUsageCores []float64   `json:"cpu_usage_cores"` // Per-core usage (0.0 to 1.0)
	Temperature   float32     `json:"temperature"`     // In Celsius (if available)
	Uptime        uint64      `json:"uptime"`          // In seconds
	// ... Add other fields as needed ...
}

// ReadableTranslatorStatus is used for formatted output.
type ReadableTranslatorStatus struct {
	RAMUsage      string            `json:"ram_usage"`
	BufferUsage   BufferUsage       `json:"buffer_usage"`
	CPUUsageCores map[string]string `json:"cpu_usage_cores"` // Changed to string
	Temperature   string            `json:"temperature"`
	Uptime        string            `json:"uptime"`
}

// --- Callback Types ---
type PublishFunc func(topic, payload string) error
type ConfigHandlerFunc func(payload string)

// Client struct definition
type Client struct {
	pahoClient MQTT.Client
	debug      bool
	brokerURL  string // Store broker URL for reconnects
	clientID   string
	user       string
	pw         string
}
type BufferUsage struct {
	UsedMB      int     // Buffer/cache used in MB
	AvailableMB int     // Available memory in MB
	UsedPercent float64 // Percentage of total memory used by buffers/cache
}

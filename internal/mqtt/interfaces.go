package mqtt

import MQTT "github.com/eclipse/paho.mqtt.golang"

type MessageHandler interface {
	HandleMessage(client MQTT.Client, msg MQTT.Message)
}
type ClockConfigPayload struct {
	Takt *uint8 `json:"takt"`
}


type TranslatorStatus struct {
	RAMUsage      uint64      `json:"ram_usage"`       
	BufferUsage   BufferUsage `json:"buffer_usage"`    
	CPUUsageCores []float64   `json:"cpu_usage_cores"` 
	Temperature   float32     `json:"temperature"`     
	Uptime        uint64      `json:"uptime"`          
	
}

type ReadableTranslatorStatus struct {
	RAMUsage      string            `json:"ram_usage"`
	BufferUsage   BufferUsage       `json:"buffer_usage"`
	CPUUsageCores map[string]string `json:"cpu_usage_cores"` 
	Temperature   string            `json:"temperature"`
	Uptime        string            `json:"uptime"`
}

type PublishFunc func(topic, payload string) error
type ConfigHandlerFunc func(payload string)

type Client struct {
	pahoClient MQTT.Client
	debug      bool
	brokerURL  string 
	clientID   string
	user       string
	pw         string
}
type BufferUsage struct {
	UsedMB      int     
	AvailableMB int     
	UsedPercent float64 
}

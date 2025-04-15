// internal/mqtt/interfaces.go
package mqtt

import MQTT "github.com/eclipse/paho.mqtt.golang"

// MessageHandler defines the standard message handling capability needed by the MQTT client
// for messages that are NOT configuration updates.
type MessageHandler interface {
	HandleMessage(client MQTT.Client, msg MQTT.Message)
}

// Note: ConfigHandlerFunc type is defined directly in mqtt_client.go now.

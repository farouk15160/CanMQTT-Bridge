package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/farouk15160/Translater-code-new/internal/canframe"
	"github.com/farouk15160/Translater-code-new/internal/config"
	myMqtt "github.com/farouk15160/Translater-code-new/internal/mqtt"
)

func main() {
	// Example usage: adjust broker URL as needed.
	brokerURL := "tcp://192.168.178.5:1883"
	clientID := "farouk_mqtt"
	debug := true

	// 1) Load the config from JSON
	cfgPath := "configs/messages_config.json"
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("Error loading config at %s: %v", cfgPath, err)
	}
	log.Printf("Loaded %d can2mqtt configs, %d mqtt2can configs from %s.",
		len(cfg.Can2mqtt), len(cfg.Mqtt2can), cfgPath)

	// 2) Connect to the MQTT broker
	mqttClient := myMqtt.NewClient(brokerURL, clientID, debug)
	if err := mqttClient.Connect(brokerURL, clientID); err != nil {
		log.Fatalf("Failed to connect to MQTT: %v", err)
	}

	// 3) Subscribe to topics from the config (example with can2mqtt)
	for _, conv := range cfg.Can2mqtt {
		if err := mqttClient.Subscribe(conv.Topic); err != nil {
			log.Printf("Failed to subscribe to %s: %v", conv.Topic, err)
		} else {
			log.Printf("Subscribed to: %s", conv.Topic)
		}
	}

	// (Similarly, you might subscribe to Mqtt2can topics if you want.)

	// 4) Example usage of the canframe package
	exampleFrame := canframe.Frame{
		ID:     0x123,
		Length: 8,
		Data:   [8]uint8{1, 2, 3, 4, 5, 6, 7, 8},
	}
	fmt.Printf("Example CAN Frame: %+v\n", exampleFrame)

	// 5) (Optional) Publish a test message to verify your subscription:
	// mqttClient.Publish("test/topic/abc", "Hello from Go!")

	// 6) Wait for CTRL+C or kill signal
	waitForSignal()

	// 7) Disconnect cleanly from MQTT
	mqttClient.Disconnect()
	log.Println("Shutting down gracefully...")
}

// waitForSignal blocks until an OS signal is received for termination.
func waitForSignal() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan // block until signal
}

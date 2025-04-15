package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	MQTT "github.com/eclipse/paho.mqtt.golang" // Import paho mqtt library
	"github.com/farouk15160/Translater-code-new/internal/bridge"
	"github.com/farouk15160/Translater-code-new/internal/config" // Import config package
	myMqtt "github.com/farouk15160/Translater-code-new/internal/mqtt"
)

func main() {
	flag.Parse() // Parse command-line flags

	log.Println("--- Starting CAN-MQTT Translator ---")

	// 1. Apply settings from flags/defaults to the bridge package
	bridge.SetDbg(*config.DebugFlag)
	bridge.SetCi(*config.CanIfaceFlag)
	bridge.SetCs(*config.MqttBrokerFlag)                          // Store broker URL for info, MQTT client handles connection string itself
	bridge.SetC2mf(*config.ConfigFileFlag)                        // Set initial config file path
	bridge.SetConfDirMode(fmt.Sprintf("%d", *config.DirModeFlag)) // Convert int flag to string setter expects
	bridge.SetUserName(*config.UsernameFlag)
	bridge.SetTimeSleepValue(fmt.Sprintf("%d", *config.SleepTimeFlag)) // Convert int flag to string setter expects
	bridge.SetThread(*config.ThreadFlag)

	// 2. Load the initial configuration file (using the path set in bridge)
	initialConfigPath := bridge.GetConfigFilePath() // Get the path (potentially expanded)
	cfg, err := config.LoadConfig(initialConfigPath)
	if err != nil {
		log.Fatalf("Error loading initial config at %s: %v", initialConfigPath, err)
	}
	log.Printf("Loaded %d can2mqtt rules, %d mqtt2can rules from %s.",
		len(cfg.Can2mqtt), len(cfg.Mqtt2can), initialConfigPath)

	// Make the loaded config accessible to the bridge (e.g., conversion functions)
	bridge.SetConfig(cfg)

	// 3. Initialize MQTT Client
	mqttClient, err := myMqtt.NewClientAndConnect(*config.ClientIDFlag, bridge.GetBrokerURL(), bridge.IsDebugEnabled())
	if err != nil {
		log.Fatalf("Failed to initialize and connect MQTT client: %v", err)
	}
	defer mqttClient.Disconnect() // Ensure disconnection on exit

	// 4. Wire up handlers and publishers using callbacks
	log.Println("Wiring up MQTT handlers and publisher...")

	bridge.SetMQTTPublisher(mqttClient.Publish)

	myMqtt.SetBridgeMessageHandler(bridgeMsgHandler{}) // Pass an instance

	myMqtt.SetConfigHandler(bridge.ApplyConfigUpdate)

	// 5. Subscribe to initial topics based on the loaded config (MQTT->CAN rules)
	log.Printf("Subscribing to %d initial MQTT topics...", len(cfg.Mqtt2can))
	for _, conv := range cfg.Mqtt2can {
		if err := mqttClient.Subscribe(conv.Topic); err != nil {
			log.Printf("Failed to subscribe to %s: %v", conv.Topic, err)
		} else {
			if bridge.IsDebugEnabled() {
				log.Printf("MQTT Subscribed to: %s", conv.Topic)
			}
		}
	}

	// 6. Publish the retained "start" info
	myMqtt.PublishStartInfo(mqttClient)

	// 7. Start the main bridge logic (CAN handling, etc.)
	log.Println("Starting bridge main loop...")
	bridge.Start(mqttClient.Subscribe)

	// 8. Wait for shutdown signal (bridge.Start should handle this, but keep fallback)
	log.Println("Bridge exited or setup failed before start. Waiting for signal...")
	waitForSignal()
	log.Println("Shutdown signal received. Exiting.")
}

// bridgeMsgHandler satisfies MQTT.MessageHandler interface by calling bridge.HandleMessage
type bridgeMsgHandler struct{}

func (bh bridgeMsgHandler) HandleMessage(client MQTT.Client, msg MQTT.Message) {
	bridge.HandleMessage(client, msg)
}

// waitForSignal blocks until an OS signal is received for termination.
func waitForSignal() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan // block until signal
}

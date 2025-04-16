package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	// Only needed if adding delays/timeouts here
	MQTT "github.com/eclipse/paho.mqtt.golang" // Alias not strictly needed but can clarify origin
	bridge "github.com/farouk15160/Translater-code-new/internal/bridge"
	config "github.com/farouk15160/Translater-code-new/internal/config"
	myMqtt "github.com/farouk15160/Translater-code-new/internal/mqtt"
)

func main() {
	flag.Parse() // Parse command-line Args
	log.Println("--- Starting CAN-MQTT Translator ---")

	// 1. Apply settings from flags/defaults to the bridge package
	bridge.SetDbg(*config.DebugFlag)
	bridge.SetCi(*config.CanIfaceFlag)
	bridge.SetCs(*config.MqttBrokerFlag)   // Store broker URL for info
	bridge.SetC2mf(*config.ConfigFileFlag) // Set initial config file path
	bridge.SetConfDirMode(fmt.Sprintf("%d", *config.DirModeFlag))
	bridge.SetUserName(*config.UsernameFlag)
	bridge.SetTimeSleepValue(fmt.Sprintf("%d", *config.SleepTimeFlag))
	bridge.SetThread(*config.ThreadFlag)

	// 2. Load the initial configuration file
	initialConfigPath := bridge.GetConfigFilePath()
	cfg, err := config.LoadConfig(initialConfigPath)
	if err != nil {
		log.Fatalf("Error loading initial config at %s: %v", initialConfigPath, err)
	}
	log.Printf("Loaded %d can2mqtt rules, %d mqtt2can rules from %s.",
		len(cfg.Can2mqtt), len(cfg.Mqtt2can), initialConfigPath)

	// Make the loaded config accessible globally within the bridge package
	bridge.SetConfig(cfg)

	// 3. Initialize MQTT Client
	// *** Removed retry loop - rely on Paho's internal retry mechanism ***
	mqttClient, err := myMqtt.NewClientAndConnect(*config.ClientIDFlag, bridge.GetBrokerURL(), bridge.IsDebugEnabled())
	if err != nil {
		// Log the error from setup, but don't exit. Paho will retry in background.
		log.Printf("Warning: MQTT client setup reported an error: %v. Connection will be attempted in background.", err)
		// If client is nil, we have a fundamental setup problem, should probably exit.
		if mqttClient == nil {
			log.Fatalf("MQTT client initialization failed critically.")
		}
	} else {
		log.Println("MQTT Client initialized. Connection attempts running in background.")
	}
	// Defer disconnect regardless of initial connection state
	defer mqttClient.Disconnect()

	// 4. Wire up handlers and publishers using callbacks
	log.Println("Wiring up MQTT handlers and publisher...")
	bridge.SetMQTTPublisher(mqttClient.Publish) // Use non-retained publish for CAN->MQTT
	myMqtt.SetBridgeMessageHandler(bridgeMsgHandler{})
	myMqtt.SetConfigHandler(bridge.ApplyConfigUpdate)

	// 5. Subscribe to initial topics
	// Subscriptions will be attempted even if not connected yet; Paho should queue them.
	// The onConnect handler in mqtt_client ensures re-subscription for command topics.
	// Bridge topics should be handled by config reload mechanism if needed after connection.
	log.Printf("Attempting to subscribe to %d initial MQTT->CAN topics...", len(cfg.Mqtt2can))
	initialSubscriptionErrors := 0
	for _, conv := range cfg.Mqtt2can {
		if err := mqttClient.Subscribe(conv.Topic); err != nil {
			log.Printf("Warning: Failed initial subscribe attempt for %s: %v (will retry on connect)", conv.Topic, err)
			initialSubscriptionErrors++
		}
	}
	log.Println("Attempting to subscribe to internal command topics...")
	if err := mqttClient.Subscribe("translater/process"); err != nil {
		log.Printf("Warning: Failed initial subscribe attempt for translater/process: %v", err)
		initialSubscriptionErrors++
	}
	if err := mqttClient.Subscribe("translater/run"); err != nil {
		log.Printf("Warning: Failed initial subscribe attempt for translater/run: %v", err)
		initialSubscriptionErrors++
	}
	if initialSubscriptionErrors > 0 {
		log.Printf("Note: %d initial subscriptions failed, they will be retried upon successful MQTT connection via the onConnect handler.", initialSubscriptionErrors)
	}

	// 6. Publish the retained "start" info
	// This might fail if not connected yet, but Paho might queue it.
	// Alternatively, move this into the onConnect handler? For now, try immediately.
	myMqtt.PublishStartInfo(mqttClient)

	// 7. Start the main bridge logic (CAN handling, etc.)
	log.Println("Starting bridge main loop...")
	// Pass the MQTT subscribe function so the bridge can re-subscribe on config reload
	bridge.Start(mqttClient.Subscribe) // Bridge Start will block or run in background

	// 8. Wait for shutdown signal
	log.Println("Bridge start routine finished or running in background. Waiting for shutdown signal...")
	waitForSignal()
	log.Println("Shutdown signal received. Cleaning up...")
	// mqttClient.Disconnect() // defer handles this
	log.Println("Exiting.")
}

// bridgeMsgHandler satisfies the mqtt.MessageHandler interface
type bridgeMsgHandler struct{}

// HandleMessage delegates to the bridge's message handler
func (bh bridgeMsgHandler) HandleMessage(client MQTT.Client, msg MQTT.Message) {
	// The actual processing happens in a goroutine launched by the MQTT client's defaultHandler
	// This call just passes the necessary info.
	bridge.HandleMessage(client, msg)
}

// waitForSignal blocks until an OS signal (Interrupt or Terminate) is received.
func waitForSignal() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	receivedSignal := <-sigChan // block until signal
	log.Printf("Signal listener started. Signal received: %v", receivedSignal)
}

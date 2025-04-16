package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime" // For setting GOMAXPROCS
	"syscall"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	bridge "github.com/farouk15160/Translater-code-new/internal/bridge"
	config "github.com/farouk15160/Translater-code-new/internal/config"
	myMqtt "github.com/farouk15160/Translater-code-new/internal/mqtt"
)

func main() {
	// Optimize for concurrency by using all available CPU cores
	runtime.GOMAXPROCS(runtime.NumCPU())

	flag.Parse()
	log.Println("--- Starting CAN-MQTT Translator ---")

	// 1. Apply settings from flags/defaults
	bridge.SetDbg(*config.DebugFlag)
	bridge.SetCi(*config.CanIfaceFlag)
	bridge.SetCs(*config.MqttBrokerFlag)
	bridge.SetC2mf(*config.ConfigFileFlag) // Set initial path -> triggers initial LoadConfig -> SetConfig (which preprocesses)
	bridge.SetConfDirMode(fmt.Sprintf("%d", *config.DirModeFlag))
	bridge.SetUserName(*config.UsernameFlag)
	bridge.SetTimeSleepValue(fmt.Sprintf("%d", *config.SleepTimeFlag))
	bridge.SetThread(*config.ThreadFlag) // Note: CAN handling might still run in background if not threaded

	// 2. Load Config (happens inside SetC2mf now, re-check logic or load explicitly here)
	// Explicit load after setting path might be clearer:
	initialConfigPath := bridge.GetConfigFilePath()
	cfg, err := config.LoadConfig(initialConfigPath)
	if err != nil {
		log.Fatalf("Error loading initial config at %s: %v", initialConfigPath, err)
	}
	bridge.SetConfig(cfg) // Preprocess and store
	log.Printf("Loaded and processed %d can2mqtt, %d mqtt2can rules from %s.",
		len(cfg.Can2mqtt), len(cfg.Mqtt2can), initialConfigPath)

	// 3. Initialize MQTT Client
	mqttClient, err := myMqtt.NewClientAndConnect(*config.ClientIDFlag, bridge.GetBrokerURL(), bridge.IsDebugEnabled())
	if err != nil {
		log.Printf("Warning: MQTT client setup reported an error: %v. Connection will be attempted in background.", err)
		if mqttClient == nil {
			log.Fatalf("MQTT client initialization failed critically.")
		}
	} else {
		log.Println("MQTT Client initialized. Connection attempts running in background.")
	}
	defer mqttClient.Disconnect()

	// 4. Wire up handlers and publishers
	log.Println("Wiring up MQTT handlers and publisher...")
	bridge.SetMQTTPublisher(mqttClient) // Pass the client which implements the Publisher interface
	myMqtt.SetBridgeMessageHandler(bridgeMsgHandler{})
	myMqtt.SetConfigHandler(bridge.ApplyConfigUpdate)
	// Pass the MQTT client update function to the bridge if needed for username changes etc.
	// bridge.SetMqttClientUpdater(...) // If MQTT client needs dynamic updates

	// 5. Subscribe to initial topics (using the client's Subscribe method)
	// Access config via bridge's maps after SetConfig
	bridge.ConfigLock.RLock()                                       // Use exported name
	initialMqttTopics := make([]string, 0, len(bridge.MqttRuleMap)) // Use exported name
	for topic := range bridge.MqttRuleMap {                         // Use exported name
		initialMqttTopics = append(initialMqttTopics, topic)
	}
	bridge.ConfigLock.RUnlock() // Use exported name

	log.Printf("Attempting to subscribe to %d initial MQTT->CAN topics...", len(initialMqttTopics))
	initialSubscriptionErrors := 0
	for _, topic := range initialMqttTopics {
		if err := mqttClient.Subscribe(topic); err != nil {
			log.Printf("Warning: Failed initial subscribe attempt for %s: %v (will retry on connect)", topic, err)
			initialSubscriptionErrors++
		}
	}
	// Subscribe to internal command topics
	internalTopics := []string{"translater/process", "translater/run"}
	log.Println("Attempting to subscribe to internal command topics...")
	for _, topic := range internalTopics {
		if err := mqttClient.Subscribe(topic); err != nil {
			log.Printf("Warning: Failed initial subscribe attempt for %s: %v", topic, err)
			initialSubscriptionErrors++
		}
	}
	if initialSubscriptionErrors > 0 {
		log.Printf("Note: %d initial subscriptions failed, they will be retried upon successful MQTT connection via the onConnect handler.", initialSubscriptionErrors)
	}

	// 6. Publish retained "start" info
	myMqtt.PublishStartInfo(mqttClient)

	// 7. Start the main bridge logic (CAN handling, worker pools)
	log.Println("Starting bridge...")
	// Pass subscribe/unsubscribe functions
	bridge.Start(mqttClient.Subscribe, mqttClient.Unsubscribe) // Bridge Start now starts workers and CAN handler

	// 8. Wait for shutdown signal
	log.Println("Bridge routines started. Waiting for shutdown signal (Ctrl+C)...")
	waitForSignal()
	log.Println("Shutdown signal received.")

	// 9. Trigger graceful shutdown in bridge (stops workers, CAN)
	bridge.Stop()

	// mqttClient.Disconnect() // defer handles this
	log.Println("Cleanup complete. Exiting.")
}

// bridgeMsgHandler satisfies the mqtt.MessageHandler interface
type bridgeMsgHandler struct{}

// HandleMessage delegates to the bridge's dispatcher
func (bh bridgeMsgHandler) HandleMessage(client MQTT.Client, msg MQTT.Message) {
	// bridge.HandleMessage now dispatches to the mqttWorkChan
	bridge.HandleMessage(client, msg)
}

// waitForSignal blocks until an OS signal is received.
func waitForSignal() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	receivedSignal := <-sigChan
	log.Printf("Signal received: %v", receivedSignal)
}

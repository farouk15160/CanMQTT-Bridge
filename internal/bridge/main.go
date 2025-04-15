package bridge

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	// Import config package for struct definitions
	"github.com/brutella/can"
	"github.com/farouk15160/Translater-code-new/internal/config"
)

// --- Bridge State Variables ---
var (
	// Settings (initialized by flags/defaults in main, modifiable via MQTT)
	mqttUsername   string        = "farouk"
	timeSleepValue time.Duration = 0 * time.Microsecond
	runInThread    bool          = false
	debugMode      bool          = false
	canInterface   string        = "can0"
	mqttBrokerURL  string        = "tcp://localhost:1883" // Informational, connection handled by MQTT client
	configFilePath string                                 // Set initially, potentially changed by MQTT
	directionMode  int           = 0                      // 0=bidir, 1=c2m, 2=m2c

	// Runtime state
	loadedConfig *config.Config        // Holds the parsed config from the JSON file
	lastClock    string         = "00" // Last timestamp from clock message
	wg           sync.WaitGroup
	bus          *can.Bus // CAN bus instance from canbushandling.go
	csi          []uint32 // subscribed CAN IDs slice from canbushandling.go

	// Callbacks/Interfaces
	mqttPublisher Publisher          // Interface for publishing MQTT messages
	mqttSubscribe func(string) error // Function to subscribe to MQTT topics (passed during Start)
)

// --- Initialization and Setup ---

// init ensures the default config file path is expanded immediately.
func init() {
	configFilePath = getDefaultConfigPath("~/go-exe/testconfig.json")
}

// Helper function to expand ~ in path
func getDefaultConfigPath(relativePath string) string {
	if strings.HasPrefix(relativePath, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			log.Printf("Warning: Could not get user home directory: %v. Using relative path.", err)
			return strings.TrimPrefix(relativePath, "~/")
		}
		return filepath.Join(homeDir, strings.TrimPrefix(relativePath, "~/"))
	}
	return relativePath
}

// SetConfig stores the loaded configuration.
func SetConfig(cfg *config.Config) {
	if cfg == nil {
		log.Println("Warning: Bridge SetConfig called with nil config.")
	}
	loadedConfig = cfg
}

// --- Getters for settings (used by other packages if needed) ---
func GetConfigFilePath() string {
	return configFilePath
}
func GetBrokerURL() string {
	return mqttBrokerURL
}
func IsDebugEnabled() bool {
	return debugMode
}

// --- Setters for Bridge Configuration (called by main initially, and ApplyConfigUpdate) ---

func SetDbg(v bool) {
	debugMode = v
	log.Printf("Bridge Setting: Debug Mode set to: %t", debugMode)
}

func SetCi(c string) {
	canInterface = c
	log.Printf("Bridge Setting: CAN Interface set to: %s", canInterface)
}

// SetC2mf updates the config file path and triggers a reload.
func SetC2mf(f string) {
	newPath := f
	// Expand ~ if present
	if strings.HasPrefix(f, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			log.Printf("Warning: Could not get user home directory for path '%s': %v. Using relative path.", f, err)
			newPath = strings.TrimPrefix(f, "~/")
		} else {
			newPath = filepath.Join(homeDir, strings.TrimPrefix(f, "~/"))
		}
	}

	if newPath != configFilePath {
		log.Printf("Bridge Setting: Config file path changed to: %s", newPath)
		configFilePath = newPath
		// Reload the configuration
		ReloadConfig()
	} else {
		log.Printf("Bridge Setting: Config file path '%s' is already set.", newPath)
	}
}

// SetCs stores the MQTT broker URL (primarily for informational purposes).
func SetCs(s string) {
	mqttBrokerURL = s
	log.Printf("Bridge Setting: MQTT Broker URL set to: %s", mqttBrokerURL)

}

func SetTimeSleepValue(s string) {
	duration, err := strconv.Atoi(s)
	if err != nil {
		log.Printf("Error converting sleep time value '%s' to int: %v", s, err)
		return
	}
	if duration < 0 {
		log.Printf("Warning: Received negative sleep time value %d. Setting to 0.", duration)
		duration = 0
	}
	timeSleepValue = time.Duration(duration) * time.Microsecond
	log.Printf("Bridge Setting: Time Sleep Value set to %v", timeSleepValue)
}

func SetThread(t bool) {
	runInThread = t
	log.Printf("Bridge Setting: Run CAN In Thread set to: %t", runInThread)
}

func SetConfDirMode(s string) {
	modeVal, err := strconv.Atoi(s)
	if err != nil {
		log.Printf("Error converting dirMode value '%s' to int: %v", s, err)
		return
	}
	if modeVal >= 0 && modeVal <= 2 {
		directionMode = modeVal
		log.Printf("Bridge Setting: Direction Mode set to: %d", directionMode)
	} else {
		log.Printf("Error: got invalid value for dirMode (%s). Valid values are 0, 1, or 2", s)
	}
}

func SetUserName(s string) {
	mqttUsername = s
	log.Printf("Bridge Setting: MQTT Username set to: %s", mqttUsername)
}

// --- Main Bridge Logic ---

// Start initializes CAN connection and waits. MQTT connection is assumed to be handled externally.
// It receives the MQTT subscribe function to allow reloading subscriptions.
func Start(subscribeFunc func(string) error) {
	fmt.Println("--- Bridge Starting ---")
	fmt.Println("Initial Configuration:")
	fmt.Println("  MQTT Broker URL:", mqttBrokerURL)
	fmt.Println("  MQTT Username:", mqttUsername)
	fmt.Println("  CAN Interface:", canInterface)
	fmt.Println("  Config File:", configFilePath)
	fmt.Print("  Direction Mode:", directionMode, " (")
	switch directionMode {
	case 0:
		fmt.Println("bidirectional)")
	case 1:
		fmt.Println("can2mqtt only)")
	case 2:
		fmt.Println("mqtt2can only)")
	}
	fmt.Printf("  Debug Mode:    %t\n", debugMode)
	fmt.Printf("  Sleep Time:    %v\n", timeSleepValue)
	fmt.Printf("  Threading:     %t\n", runInThread)
	fmt.Println("-------------------------")

	// Store the subscribe function for reloading
	mqttSubscribe = subscribeFunc
	if mqttSubscribe == nil {
		log.Println("Warning: Bridge Start called with nil MQTT subscribe function. Config reload might fail.")
	}

	// Subscribe to initial CAN IDs based on loaded config
	subscribeInitialCanIDs()

	// Start CAN handling
	wg.Add(1)
	startCanHandling(canInterface) // This might run in a goroutine or block

	log.Println("Bridge started. Waiting for tasks or signals...")
	wg.Wait() // Wait for CAN handling (and potentially other tasks) to finish
	log.Println("Bridge main loop finished.")
}

// ReloadConfig attempts to reload the configuration from the current file path.
func ReloadConfig() {
	log.Printf("Attempting to reload configuration from: %s", configFilePath)

	// Load new config file
	newCfg, err := config.LoadConfig(configFilePath)
	if err != nil {
		log.Printf("Error reloading config file '%s': %v. Keeping previous config.", configFilePath, err)
		return // Keep the old config if loading fails
	}

	log.Printf("Successfully loaded new config: %d can2mqtt, %d mqtt2can rules.",
		len(newCfg.Can2mqtt), len(newCfg.Mqtt2can))

	// Store the new config
	SetConfig(newCfg)

	// --- Update Subscriptions ---
	// TODO: Unsubscribe from old topics/IDs if necessary. This requires tracking.
	// For simplicity now, we just add new subscriptions. Duplicates might be handled by MQTT/CAN libs.

	// Subscribe to new MQTT topics (MQTT->CAN)
	if mqttSubscribe != nil {
		log.Printf("Subscribing to %d MQTT topics from new config...", len(newCfg.Mqtt2can))
		for _, conv := range newCfg.Mqtt2can {
			if err := mqttSubscribe(conv.Topic); err != nil {
				log.Printf("Reload: Failed to subscribe to %s: %v", conv.Topic, err)
			} else {
				if debugMode {
					log.Printf("Reload: MQTT Subscribed to: %s", conv.Topic)
				}
			}
		}
	} else {
		log.Println("Reload Error: Cannot subscribe to MQTT topics, subscribe function is not set.")
	}

	// Subscribe to new CAN IDs (CAN->MQTT)
	subscribeInitialCanIDs() // Resubscribes based on the *new* loadedConfig

	log.Println("Configuration reload process completed.")
}

// subscribeInitialCanIDs clears and subscribes to CAN IDs based on the current loadedConfig.
func subscribeInitialCanIDs() {
	if loadedConfig == nil {
		log.Println("Cannot subscribe CAN IDs: config not loaded.")
		return
	}

	// Clear existing CAN subscriptions
	clearCanSubscriptions()
	log.Printf("Subscribing to %d CAN IDs from config...", len(loadedConfig.Can2mqtt))

	// Add new subscriptions
	for _, canidConv := range loadedConfig.Can2mqtt {
		hexStr := canidConv.CanID
		if strings.HasPrefix(hexStr, "0x") {
			hexStr = strings.TrimPrefix(hexStr, "0x")
		}

		i, err := strconv.ParseUint(hexStr, 16, 32)
		if err != nil {
			log.Printf("Error parsing CAN ID '%s': %v. Skipping subscription.", canidConv.CanID, err)
			continue
		}
		canSubscribe(uint32(i)) // Assuming canSubscribe handles storage
		if debugMode {
			fmt.Printf("CAN Subscribed to ID: %x\n", i)
		}
	}
}

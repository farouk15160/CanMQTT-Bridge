package bridge

import (
	// Added for encoding timestamp
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime" // Added for NumCPU
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brutella/can"
	"github.com/farouk15160/Translater-code-new/internal/config"
)

// --- Bridge State Variables ---
var (
	// Settings
	mqttUsername   string        = "farouk"
	timeSleepValue time.Duration = 0 * time.Microsecond
	currentBitSize int           = 8
	runInThread    bool          = false
	debugMode      bool          = false
	canInterface   string        = "can0"
	ip_adress      string        = "192.168.178.5"
	mqttBrokerURL  string        = "mqtt://" + ip_adress + ":1883"
	configFilePath string
	directionMode  int   = 0
	numWorkers     int   = runtime.NumCPU() // Number of worker goroutines per poo
	clockTakt      uint8 = 10               // ADDED: Clock frequency in Hz (default 10)

	// Runtime state
	loadedConfig *config.Config // Holds the raw parsed config
	// --- EXPORTED ---
	CanRuleMap  map[uint32]*config.Conversion // Preprocessed CAN ID -> Rule map
	MqttRuleMap map[string]*config.Conversion // Preprocessed MQTT Topic -> Rule map
	ConfigLock  sync.RWMutex                  // Mutex for accessing config maps safely
	// --- END EXPORTED ---
	lastClock string = "00" // Last timestamp from clock message
	wg        sync.WaitGroup
	bus       *can.Bus
	csi       []uint32 // subscribed CAN IDs slice (still needed for filtering in handleCANFrame)

	// Concurrency (Worker Pools)
	canWorkChan   chan can.Frame    // Channel for CAN frames -> workers
	mqttWorkChan  chan MqttWorkItem // Channel for MQTT messages -> workers
	stopChan      chan struct{}     // Channel to signal workers to stop
	clockStopChan chan struct{}     // ADDED: Channel to signal Clock Sender to stop

	// Callbacks/Interfaces
	mqttPublisher     Publisher          // Interface for publishing MQTT messages
	mqttSubscribe     func(string) error // Function to subscribe to MQTT topics
	mqttUnsubscribe   func(string) error // Function to unsubscribe from MQTT topics
	mqttClientUpdater func(string)
)

// MqttWorkItem struct to pass MQTT data to workers
type MqttWorkItem struct {
	Topic   string
	Payload []byte
}

// --- Initialization and Setup ---

func init() {
	configFilePath = getDefaultConfigPath("~/go-exe/testconfig.json")
}

func SetBitSize(bits int) {
	ConfigLock.Lock()         // Use exported name
	defer ConfigLock.Unlock() // Use exported name

	var bytes int
	switch bits {
	case 8:
		bytes = 1
	case 16:
		bytes = 2
	case 32:
		bytes = 4
	case 64:
		bytes = 8
	case 128:
		bytes = 16
	case 256:
		bytes = 32
	// Add cases for 24, 40, 48, 56 if needed, mapping to bytes 3, 5, 6, 7
	default:
		log.Printf("Bridge Warning: Invalid bit_size %d received. Must be 8, 16, 32, 64, 128, 256. Using previous value %d bytes.", bits, currentBitSize)
		return // Keep the current value if input is invalid
	}

	// Clamp to valid DLC range (0-8 for standard CAN, though 0 is unusual for data)
	// We'll clamp 1-8 here as 0 bytes is unlikely intended via this setting.
	if bytes < 1 {
		bytes = 1
	} else if bytes > 8 {
		log.Printf("Bridge Warning: Requested bit_size %d results in %d bytes, exceeding standard CAN limit. Clamping to 8 bytes.", bits, bytes)
		bytes = 8
	}

	if bytes != currentBitSize {
		log.Printf("Bridge Setting: Current Bit Size (DLC) set to: %d bytes (from %d bits)", bytes, bits)
		currentBitSize = bytes
	}
}
func GetCurrentBitSize() int {
	ConfigLock.RLock()
	defer ConfigLock.RUnlock()
	return currentBitSize
}

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

// SetConfig preprocesses and stores the configuration.
func SetConfig(cfg *config.Config) {
	if cfg == nil {
		log.Println("Warning: Bridge SetConfig called with nil config.")
		cfg = &config.Config{} // Use empty config to avoid nil panics
	}

	// --- Preprocess rules into maps for efficient lookup ---
	newCanRuleMap := make(map[uint32]*config.Conversion)
	for i := range cfg.Can2mqtt {
		rule := &cfg.Can2mqtt[i] // Use pointer to avoid copying
		canidStr := strings.TrimPrefix(rule.CanID, "0x")
		canidNr, err := strconv.ParseUint(canidStr, 16, 32)
		if err != nil {
			log.Printf("SetConfig Warning: Invalid CAN ID '%s' in can2mqtt rule for topic '%s': %v. Skipping rule.", rule.CanID, rule.Topic, err)
			continue
		}
		// TODO: Add validation for PayloadField.Place here if desired
		newCanRuleMap[uint32(canidNr)] = rule
	}

	newMqttRuleMap := make(map[string]*config.Conversion)
	for i := range cfg.Mqtt2can {
		rule := &cfg.Mqtt2can[i] // Use pointer
		// TODO: Add validation for PayloadField.Place here if desired
		newMqttRuleMap[rule.Topic] = rule
	}

	// --- Safely update the global maps and config ---
	ConfigLock.Lock() // Use exported name
	loadedConfig = cfg
	CanRuleMap = newCanRuleMap   // Use exported name
	MqttRuleMap = newMqttRuleMap // Use exported name
	ConfigLock.Unlock()          // Use exported name

	log.Printf("Bridge: Processed config - %d can2mqtt rules mapped, %d mqtt2can rules mapped.", len(CanRuleMap), len(MqttRuleMap)) // Use exported names
}

func GetConfigFilePath() string {
	// Safe to read configFilePath without lock if only set at init and via SetC2mf (which locks)
	return configFilePath
}
func GetBrokerURL() string {
	ConfigLock.RLock() // Lock if broker URL can be changed dynamically
	url := mqttBrokerURL
	ConfigLock.RUnlock()
	return url
}
func IsDebugEnabled() bool {
	ConfigLock.RLock() // Lock for safe concurrent read
	dbg := debugMode
	ConfigLock.RUnlock()
	return dbg
}

// --- Setters for Bridge Configuration ---

func SetDbg(v bool) {
	ConfigLock.Lock() // Use exported name
	debugMode = v
	ConfigLock.Unlock() // Use exported name
	log.Printf("Bridge Setting: Debug Mode set to: %t", debugMode)
}

func SetIp(v string) {
	ConfigLock.Lock() // Use exported name
	ip_adress = v
	ConfigLock.Unlock() // Use exported name
	log.Printf("Broker Ip: %s", mqttBrokerURL)
}

func SetCi(c string) {
	// Assuming CAN interface changes require restart, not changed dynamically
	ConfigLock.Lock() // Use exported name
	canInterface = c
	ConfigLock.Unlock() // Use exported name
	log.Printf("Bridge Setting: CAN Interface set to: %s", canInterface)
}

func SetC2mf(f string) {
	newPath := f
	if strings.HasPrefix(f, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			log.Printf("Warning: Could not get user home directory for path '%s': %v. Using relative path.", f, err)
			newPath = strings.TrimPrefix(f, "~/")
		} else {
			newPath = filepath.Join(homeDir, strings.TrimPrefix(f, "~/"))
		}
	}

	// Check needs lock if configFilePath can be read elsewhere concurrently
	ConfigLock.RLock() // Use exported name
	currentPath := configFilePath
	ConfigLock.RUnlock() // Use exported name

	if newPath != currentPath {
		log.Printf("Bridge Setting: Config file path changed to: %s", newPath)
		ConfigLock.Lock() // Use exported name
		configFilePath = newPath
		ConfigLock.Unlock() // Use exported name
		// Reload the configuration
		ReloadConfig() // ReloadConfig handles its own locking
	} else {
		// log.Printf("Bridge Setting: Config file path '%s' is already set.", newPath) // Reduce noise
	}
}

func SetCs(s string) {
	ConfigLock.Lock() // Use exported name
	mqttBrokerURL = s
	ConfigLock.Unlock() // Use exported name
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
	ConfigLock.Lock() // Use exported name
	timeSleepValue = time.Duration(duration) * time.Microsecond
	ConfigLock.Unlock() // Use exported name
	log.Printf("Bridge Setting: Time Sleep Value set to %v", timeSleepValue)
}

func SetThread(t bool) {
	// Assuming this is set at startup only
	ConfigLock.Lock() // Use exported name
	runInThread = t
	ConfigLock.Unlock() // Use exported name
	log.Printf("Bridge Setting: Run CAN In Thread set to: %t", runInThread)
}

func SetConfDirMode(s string) {
	modeVal, err := strconv.Atoi(s)
	if err != nil {
		log.Printf("Error converting dirMode value '%s' to int: %v", s, err)
		return
	}
	if modeVal >= 0 && modeVal <= 2 {
		ConfigLock.Lock() // Use exported name
		directionMode = modeVal
		ConfigLock.Unlock() // Use exported name
		log.Printf("Bridge Setting: Direction Mode set to: %d", directionMode)
	} else {
		log.Printf("Error: got invalid value for dirMode (%s). Valid values are 0, 1, or 2", s)
	}
}

func SetUserName(s string) {
	ConfigLock.Lock() // Use exported name
	mqttUsername = s
	ConfigLock.Unlock() // Use exported name
	log.Printf("Bridge Setting: MQTT Username set to: %s", mqttUsername)
	if mqttClientUpdater != nil {
		mqttClientUpdater(mqttUsername)
	} else {
		log.Println("Warning: MQTT client updater not set. Username might not be applied.")
	}
}

func SetMqttClientUpdater(updater func(string)) {
	mqttClientUpdater = updater
}

// --- Main Bridge Logic ---

// Start initializes CAN, MQTT callbacks, worker pools, and waits.
func Start(subFunc func(string) error, unsubFunc func(string) error) {
	fmt.Println("--- Bridge Starting ---")
	// Print initial config... (as before)
	fmt.Printf("  Clock Takt:    %d Hz\n", GetClockTakt()) // Add clock takt to initial printout
	fmt.Println("-------------------------")
	fmt.Println("Initial Configuration:")
	fmt.Println("  MQTT Broker URL:", GetBrokerURL()) // Use getter for thread safety
	fmt.Println("  MQTT Username:", mqttUsername)     // Assuming username not changed dynamically often
	fmt.Println("  CAN Interface:", canInterface)
	fmt.Println("  Config File:", GetConfigFilePath())
	fmt.Print("  Direction Mode:", directionMode, " (") // Assuming dirMode not changed dynamically often
	switch directionMode {
	case 0:
		fmt.Println("bidirectional)")
	case 1:
		fmt.Println("can2mqtt only)")
	case 2:
		fmt.Println("mqtt2can only)")
	}
	fmt.Printf("  Debug Mode:    %t\n", IsDebugEnabled()) // Use getter
	fmt.Printf("  Sleep Time:    %v\n", timeSleepValue)   // Assuming sleep not changed dynamically often
	fmt.Printf("  Threading:     %t\n", runInThread)
	fmt.Printf("  Workers:       %d\n", numWorkers)
	fmt.Println("-------------------------")

	// Store the subscribe/unsubscribe functions
	mqttSubscribe = subFunc
	mqttUnsubscribe = unsubFunc
	if mqttSubscribe == nil || mqttUnsubscribe == nil {
		log.Println("Warning: Bridge Start called with nil MQTT subscribe/unsubscribe function. Config reload might fail.")
	}

	// Initialize worker/stop channels
	bufferSize := numWorkers * 2
	canWorkChan = make(chan can.Frame, bufferSize)
	mqttWorkChan = make(chan MqttWorkItem, bufferSize)
	stopChan = make(chan struct{})
	clockStopChan = make(chan struct{}) // Initialize clock stop channel

	// Subscribe to initial CAN IDs (based on preprocessed map)
	subscribeInitialCanIDs()

	// Start Worker Pools
	log.Printf("Starting %d CAN->MQTT workers...", numWorkers)
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go canProcessor(i, &wg)
	}
	log.Printf("Starting %d MQTT->CAN workers...", numWorkers)
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go mqttProcessor(i, &wg)
	}
	// --- ADDED: Start Clock Sender ---
	log.Println("Starting CAN Clock Sender...")
	wg.Add(1) // Increment waitgroup for the clock sender
	go runClockSender(&wg)
	// --- END ADDED ---

	// Start CAN handling (runs ConnectAndPublish loop)
	wg.Add(1)                      // Add task for CAN handling itself
	startCanHandling(canInterface) // This might run in a goroutine or block

	log.Println("Bridge started. Waiting for tasks or signals...")
	// Note: We don't wait on wg here, main loop waits for OS signal.
	// wg is used to ensure workers finish on shutdown.
}

// Stop signals workers and waits for them to finish.
func Stop() {
	log.Println("Bridge: Initiating shutdown...")

	// --- Safely close channels ---
	// 1. Signal Stop (prevents new work being added *after* closing channels)
	close(stopChan)      // Signal CAN/MQTT workers
	close(clockStopChan) // Signal Clock sender
	// 2. Close CAN bus (stops handleCANFrame from adding to canWorkChan)
	if bus != nil {
		log.Println("Bridge: Closing CAN bus...")
		bus.Disconnect() // Disconnect the bus
	}
	// 3. Close work channels (signals workers reading from them to stop)
	// Check if channels exist before closing
	if canWorkChan != nil {
		close(canWorkChan)
	}
	if mqttWorkChan != nil {
		close(mqttWorkChan)
	}
	// --- End Safely close channels ---

	// Wait for all worker goroutines and the CAN handler goroutine to finish
	log.Println("Bridge: Waiting for workers to finish...")
	wg.Wait()
	log.Println("Bridge: All workers stopped.")
}

// ReloadConfig reloads, preprocesses, and applies the new configuration.
func ReloadConfig() {
	log.Printf("Attempting to reload configuration from: %s", GetConfigFilePath())

	// --- Load new config file ---
	ConfigLock.RLock() // Use exported name - needed to safely read currentPath
	currentPath := configFilePath
	ConfigLock.RUnlock() // Use exported name

	newCfg, err := config.LoadConfig(currentPath)
	if err != nil {
		log.Printf("Error reloading config file '%s': %v. Keeping previous config.", currentPath, err)
		return // Keep the old config if loading fails
	}

	// --- Store old MQTT topics for unsubscribing ---
	ConfigLock.RLock() // Use exported name
	oldMqttTopics := make(map[string]struct{})
	if loadedConfig != nil { // Check if there was a previous config
		for _, rule := range loadedConfig.Mqtt2can {
			oldMqttTopics[rule.Topic] = struct{}{}
		}
	}
	ConfigLock.RUnlock() // Use exported name

	// --- Preprocess and apply the new config (updates maps) ---
	SetConfig(newCfg) // This updates exported maps internally with locks

	// --- Update Subscriptions ---
	if mqttSubscribe == nil || mqttUnsubscribe == nil {
		log.Println("Reload Error: Cannot update MQTT subscriptions, subscribe/unsubscribe functions not set.")
	} else {
		ConfigLock.RLock() // Use exported name - read new MqttRuleMap
		newMqttTopics := make(map[string]struct{})
		for topic := range MqttRuleMap { // Use exported name
			newMqttTopics[topic] = struct{}{}
		}
		currentDebugMode := debugMode // Read debug mode under lock
		ConfigLock.RUnlock()          // Use exported name

		// Unsubscribe from topics present in old config but not in new
		log.Printf("Reload: Unsubscribing from removed MQTT topics...")
		for oldTopic := range oldMqttTopics {
			if _, exists := newMqttTopics[oldTopic]; !exists {
				if err := mqttUnsubscribe(oldTopic); err != nil {
					log.Printf("Reload: Failed to unsubscribe from %s: %v", oldTopic, err)
				} else if currentDebugMode {
					log.Printf("Reload: MQTT Unsubscribed from: %s", oldTopic)
				}
			}
		}

		// Subscribe to topics present in new config but not in old (or just subscribe all new)
		log.Printf("Reload: Subscribing to new/updated MQTT topics...")
		for newTopic := range newMqttTopics {
			// Optimization: only subscribe if it wasn't in oldMqttTopics?
			// Simpler: just re-subscribe all topics from the new map. MQTT broker handles duplicates.
			if err := mqttSubscribe(newTopic); err != nil {
				log.Printf("Reload: Failed to subscribe to %s: %v", newTopic, err)
			} else if currentDebugMode {
				// log.Printf("Reload: MQTT Subscribed to: %s", newTopic) // Reduce noise
			}
		}
		log.Println("Reload: MQTT subscription update complete.")
	}

	// Update CAN subscriptions (based on the new preprocessed map)
	subscribeInitialCanIDs() // Resubscribes based on the *new* CanRuleMap

	log.Println("Configuration reload process completed.")
}

// subscribeInitialCanIDs clears and subscribes to CAN IDs based on the current CanRuleMap.
func subscribeInitialCanIDs() {
	ConfigLock.RLock()              // Use exported name - Read CanRuleMap and debugMode
	ruleCount := len(CanRuleMap)    // Use exported name
	currentCanRuleMap := CanRuleMap // Use exported name
	currentDebugMode := debugMode
	ConfigLock.RUnlock() // Use exported name

	if ruleCount == 0 {
		log.Println("Cannot subscribe CAN IDs: config not loaded or empty.")
		// Make sure existing filters are cleared if config becomes empty
		clearCanSubscriptions()
		return
	}

	// Clear existing CAN subscriptions (local list 'csi')
	clearCanSubscriptions() // Uses its own lock
	log.Printf("Subscribing to %d CAN IDs from config...", ruleCount)

	// Update the local 'csi' slice used for filtering in handleCANFrame
	csiLock.Lock() // Protect write access to csi
	csi = make([]uint32, 0, ruleCount)
	for canID := range currentCanRuleMap {
		csi = append(csi, canID)
		if currentDebugMode {
			fmt.Printf("CAN Subscribed to ID: %x\n", canID)
		}
	}
	csiLock.Unlock()
	// Note: The actual CAN bus filtering might depend on how the library handles SubscribeFunc.
	// Here, we maintain `csi` for the explicit check in `handleCANFrame`.
}

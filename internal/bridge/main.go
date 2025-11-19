package bridge

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime" 
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brutella/can"
	"github.com/farouk15160/Translater-code-new/internal/config"
)


var (
	mqttUsername   string        = "farouk"
	timeSleepValue time.Duration = 0 * time.Microsecond
	currentBitSize uint          = 8
	runInThread    bool          = false
	debugMode      bool          = false
	canInterface   string        = "can0"
	ip_adress      string        = "192.168.178.5"
	mqttBrokerURL  string        = "mqtt://" + ip_adress + ":1883"
	configFilePath string
	directionMode  int   = 0
	numWorkers     int   = runtime.NumCPU() 
	clockTakt      uint8 = 10               

	loadedConfig *config.Config 
	CanRuleMap  map[uint32]*config.Conversion 
	MqttRuleMap map[string]*config.Conversion 
	ConfigLock  sync.RWMutex                  
	lastClock   int64 = 0 // Changed to int64 to store UnixNano
	wg        sync.WaitGroup
	bus       *can.Bus
	csi       []uint32 

	canWorkChan   chan can.Frame    
	mqttWorkChan  chan MqttWorkItem 
	stopChan      chan struct{}     
	clockStopChan chan struct{}     
	mqttPublisher     Publisher          
	mqttSubscribe     func(string) error 
	mqttUnsubscribe   func(string) error 
	mqttClientUpdater func(string)
)

type MqttWorkItem struct {
	Topic   string
	Payload []byte
}


func init() {
	configFilePath = getDefaultConfigPath("~/go-exe/testconfig.json")
}

func SetBitSize(bits int) {
	ConfigLock.Lock()         
	defer ConfigLock.Unlock() 

	var bytes uint
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
	default:
		log.Printf("Bridge Warning: Invalid bit_size %d received. Must be 8, 16, 32, 64, 128, 256. Using previous value %d bytes.", bits, currentBitSize)
		return 
	}

	if bytes < 1 {
		bytes = 1
	}

	if bytes != currentBitSize {
		log.Printf("Bridge Setting: Current Bit Size (DLC) set to: %d bytes (from %d bits)", bytes, bits)
		currentBitSize = bytes
	}
}
func GetCurrentBitSize() uint {
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
		cfg = &config.Config{} 
	}

	newCanRuleMap := make(map[uint32]*config.Conversion)
	for i := range cfg.Can2mqtt {
		rule := &cfg.Can2mqtt[i] 
		canidStr := strings.TrimPrefix(rule.CanID, "0x")
		canidNr, err := strconv.ParseUint(canidStr, 16, 32)
		if err != nil {
			log.Printf("SetConfig Warning: Invalid CAN ID '%s' in can2mqtt rule for topic '%s': %v. Skipping rule.", rule.CanID, rule.Topic, err)
			continue
		}
		newCanRuleMap[uint32(canidNr)] = rule
	}

	newMqttRuleMap := make(map[string]*config.Conversion)
	for i := range cfg.Mqtt2can {
		rule := &cfg.Mqtt2can[i] 
		newMqttRuleMap[rule.Topic] = rule
	}

	ConfigLock.Lock() 
	loadedConfig = cfg
	CanRuleMap = newCanRuleMap   
	MqttRuleMap = newMqttRuleMap 
	ConfigLock.Unlock()          

	log.Printf("Bridge: Processed config - %d can2mqtt rules mapped, %d mqtt2can rules mapped.", len(CanRuleMap), len(MqttRuleMap)) 
}

func GetConfigFilePath() string {
	return configFilePath
}
func GetBrokerURL() string {
	ConfigLock.RLock() 
	url := mqttBrokerURL
	ConfigLock.RUnlock()
	return url
}
func IsDebugEnabled() bool {
	ConfigLock.RLock() 
	dbg := debugMode
	ConfigLock.RUnlock()
	return dbg
}

func SetDbg(v bool) {
	ConfigLock.Lock() 
	debugMode = v
	ConfigLock.Unlock() 
	log.Printf("Bridge Setting: Debug Mode set to: %t", debugMode)
}

func SetIp(v string) {
	ConfigLock.Lock() 
	ip_adress = v
	ConfigLock.Unlock() 
	log.Printf("Broker Ip: %s", mqttBrokerURL)
}

func SetCi(c string) {
	ConfigLock.Lock() 
	canInterface = c
	ConfigLock.Unlock() 
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

	ConfigLock.RLock() 
	currentPath := configFilePath
	ConfigLock.RUnlock() 

	if newPath != currentPath {
		log.Printf("Bridge Setting: Config file path changed to: %s", newPath)
		ConfigLock.Lock() 
		configFilePath = newPath
		ConfigLock.Unlock() 
		ReloadConfig() 
	} 
}

func SetCs(s string) {
	ConfigLock.Lock() 
	mqttBrokerURL = s
	ConfigLock.Unlock() 
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
	ConfigLock.Lock() 
	timeSleepValue = time.Duration(duration) * time.Microsecond
	ConfigLock.Unlock() 
	log.Printf("Bridge Setting: Time Sleep Value set to %v", timeSleepValue)
}

func SetThread(t bool) {
	ConfigLock.Lock() 
	runInThread = t
	ConfigLock.Unlock() 
	log.Printf("Bridge Setting: Run CAN In Thread set to: %t", runInThread)
}

func SetConfDirMode(s string) {
	modeVal, err := strconv.Atoi(s)
	if err != nil {
		log.Printf("Error converting dirMode value '%s' to int: %v", s, err)
		return
	}
	if modeVal >= 0 && modeVal <= 2 {
		ConfigLock.Lock() 
		directionMode = modeVal
		ConfigLock.Unlock() 
		log.Printf("Bridge Setting: Direction Mode set to: %d", directionMode)
	} else {
		log.Printf("Error: got invalid value for dirMode (%s). Valid values are 0, 1, or 2", s)
	}
}

func SetUserName(s string) {
	ConfigLock.Lock() 
	mqttUsername = s
	ConfigLock.Unlock() 
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

func Start(subFunc func(string) error, unsubFunc func(string) error) {
	fmt.Println("--- Bridge Starting ---")
	fmt.Printf("  Clock Takt:    %d Hz\n", GetClockTakt()) 
	fmt.Println("-------------------------")
	fmt.Println("Initial Configuration:")
	fmt.Println("  MQTT Broker URL:", GetBrokerURL()) 
	fmt.Println("  MQTT Username:", mqttUsername)     
	fmt.Println("  CAN Interface:", canInterface)
	fmt.Println("  Config File:", GetConfigFilePath())
	fmt.Print("  Direction Mode:", directionMode, " (") 
	switch directionMode {
	case 0:
		fmt.Println("bidirectional)")
	case 1:
		fmt.Println("can2mqtt only)")
	case 2:
		fmt.Println("mqtt2can only)")
	}
	fmt.Printf("  Debug Mode:    %t\n", IsDebugEnabled()) 
	fmt.Printf("  Sleep Time:    %v\n", timeSleepValue)   
	fmt.Printf("  Threading:     %t\n", runInThread)
	fmt.Printf("  Workers:       %d\n", numWorkers)
	fmt.Println("-------------------------")

	mqttSubscribe = subFunc
	mqttUnsubscribe = unsubFunc
	if mqttSubscribe == nil || mqttUnsubscribe == nil {
		log.Println("Warning: Bridge Start called with nil MQTT subscribe/unsubscribe function. Config reload might fail.")
	}

	bufferSize := numWorkers * 2
	canWorkChan = make(chan can.Frame, bufferSize)
	mqttWorkChan = make(chan MqttWorkItem, bufferSize)
	stopChan = make(chan struct{})
	clockStopChan = make(chan struct{}) 

	subscribeInitialCanIDs()

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
	log.Println("Starting CAN Clock Sender...")
	wg.Add(1) 
	go runClockSender(&wg)

	wg.Add(1)                      
	startCanHandling(canInterface) 

	log.Println("Bridge started. Waiting for tasks or signals...")
}

func Stop() {
	log.Println("Bridge: Initiating shutdown...")

	close(stopChan)      
	close(clockStopChan) 
	if bus != nil {
		log.Println("Bridge: Closing CAN bus...")
		bus.Disconnect() 
	}
	if canWorkChan != nil {
		close(canWorkChan)
	}
	if mqttWorkChan != nil {
		close(mqttWorkChan)
	}

	log.Println("Bridge: Waiting for workers to finish...")
	wg.Wait()
	log.Println("Bridge: All workers stopped.")
}

func ReloadConfig() {
	log.Printf("Attempting to reload configuration from: %s", GetConfigFilePath())

	ConfigLock.RLock() 
	currentPath := configFilePath
	ConfigLock.RUnlock() 

	newCfg, err := config.LoadConfig(currentPath)
	if err != nil {
		log.Printf("Error reloading config file '%s': %v. Keeping previous config.", currentPath, err)
		return 
	}

	ConfigLock.RLock() 
	oldMqttTopics := make(map[string]struct{})
	if loadedConfig != nil { 
		for _, rule := range loadedConfig.Mqtt2can {
			oldMqttTopics[rule.Topic] = struct{}{}
		}
	}
	ConfigLock.RUnlock() 

	SetConfig(newCfg) 

	if mqttSubscribe == nil || mqttUnsubscribe == nil {
		log.Println("Reload Error: Cannot update MQTT subscriptions, subscribe/unsubscribe functions not set.")
	} else {
		ConfigLock.RLock() 
		newMqttTopics := make(map[string]struct{})
		for topic := range MqttRuleMap { 
			newMqttTopics[topic] = struct{}{}
		}
		currentDebugMode := debugMode 
		ConfigLock.RUnlock()          

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

		log.Printf("Reload: Subscribing to new/updated MQTT topics...")
		for newTopic := range newMqttTopics {
			if err := mqttSubscribe(newTopic); err != nil {
				log.Printf("Reload: Failed to subscribe to %s: %v", newTopic, err)
			} 
		}
		log.Println("Reload: MQTT subscription update complete.")
	}

	subscribeInitialCanIDs() 

	log.Println("Configuration reload process completed.")
}

func subscribeInitialCanIDs() {
	ConfigLock.RLock()              
	ruleCount := len(CanRuleMap)    
	currentCanRuleMap := CanRuleMap 
	currentDebugMode := debugMode
	ConfigLock.RUnlock() 

	if ruleCount == 0 {
		log.Println("Cannot subscribe CAN IDs: config not loaded or empty.")
		clearCanSubscriptions()
		return
	}

	clearCanSubscriptions() 
	log.Printf("Subscribing to %d CAN IDs from config...", ruleCount)

	csiLock.Lock() 
	csi = make([]uint32, 0, ruleCount)
	for canID := range currentCanRuleMap {
		csi = append(csi, canID)
		if currentDebugMode {
			fmt.Printf("CAN Subscribed to ID: %x\n", canID)
		}
	}
	csiLock.Unlock()
}
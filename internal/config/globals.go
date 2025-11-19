package config

import "flag" 

var (
	// Flags corresponding to command-line options
	DebugFlag      = flag.Bool("v", true, "Enable verbose/debug output")
	CanIfaceFlag   = flag.String("c", "vcan0", "CAN interface name (e.g., can0)")
	MqttBrokerFlag = flag.String("m", "localhost:1883", "MQTT broker connection string (e.g., tcp://user:pass@host:port)")
	ConfigFileFlag = flag.String("f", "configs/messages_config.json", "Path to the configuration JSON file")
	DirModeFlag    = flag.Int("d", 0, "Direction mode: 0=bidirectional, 1=can2mqtt only, 2=mqtt2can only")
	UsernameFlag   = flag.String("u", "farouk", "MQTT username (overrides default, used if not in broker URL)")
	SleepTimeFlag  = flag.Int("t", 0, "Time to sleep between CAN frames in microseconds")
	ThreadFlag     = flag.Bool("T", false, "Run CAN handling in a separate thread")
	ClientIDFlag   = flag.String("id", "translater-client", "MQTT client ID")
)

const AppName = "CAN-MQTT Translator"

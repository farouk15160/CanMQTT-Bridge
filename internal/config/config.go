package config

import (
	"encoding/json"
	"fmt"
	"log" // Added
	"os"
)

// PayloadField holds metadata on how to parse certain bytes in a Frame.
type PayloadField struct {
	Key    string  `json:"key"`    // Name of the field in JSON
	Type   string  `json:"type"`   // Data type (e.g., "uint8_t", "float", "string")
	Place  [2]int  `json:"place"`  // Start and End byte index (End index is exclusive for slicing, or check logic)
	Factor float64 `json:"factor"` // Multiplier/divisor for scaling
}

// Conversion describes how to go between one CAN frame and one MQTT topic/payload.
type Conversion struct {
	Topic   string         `json:"topic"`   // MQTT Topic
	CanID   string         `json:"canid"`   // CAN ID (hex string, e.g., "0x123")
	Length  int            `json:"length"`  // Expected CAN data length (0-8 for standard)
	Payload []PayloadField `json:"payload"` // List of fields within the frame/payload
}

// Config holds the entire set of conversions (CAN->MQTT, MQTT->CAN).
type Config struct {
	Can2mqtt []Conversion `json:"can2mqtt"` // Rules for converting CAN frames TO MQTT messages
	Mqtt2can []Conversion `json:"mqtt2can"` // Rules for converting MQTT messages TO CAN frames
}

// LoadConfig reads the JSON file at `filepath` and unmarshals it into a Config struct.
func LoadConfig(filepath string) (*Config, error) {
	log.Printf("Loading configuration from: %s", filepath) // Added log
	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file '%s': %w", filepath, err)
	}
	defer file.Close()

	var cfg Config
	// Use json.NewDecoder for potentially large files
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		// Try to provide more context on JSON errors if possible
		// Note: Go's default JSON error might already be informative enough
		return nil, fmt.Errorf("failed to decode JSON config from '%s': %w", filepath, err)
	}

	// Basic validation (optional but recommended)
	if len(cfg.Can2mqtt) == 0 && len(cfg.Mqtt2can) == 0 {
		log.Printf("Warning: Configuration file '%s' loaded successfully but contains no can2mqtt or mqtt2can rules.", filepath)
	} else {
		log.Printf("Config loaded successfully: %d can2mqtt, %d mqtt2can rules.", len(cfg.Can2mqtt), len(cfg.Mqtt2can))
	}

	return &cfg, nil
}

package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// PayloadField holds metadata on how to parse certain bytes in a Frame.
type PayloadField struct {
	Key    string  `json:"key"`
	Type   string  `json:"type"`
	Place  [2]int  `json:"place"`
	Factor float64 `json:"factor"`
}

// Conversion describes how to go between CAN frames and MQTT payload.
type Conversion struct {
	Topic   string         `json:"topic"`
	CanID   string         `json:"canid"`
	Length  int            `json:"length"`
	Payload []PayloadField `json:"payload"`
}

// Config holds the entire set of conversions (CAN->MQTT, MQTT->CAN).
type Config struct {
	Can2mqtt []Conversion `json:"can2mqtt"`
	Mqtt2can []Conversion `json:"mqtt2can"`
}

// LoadConfig reads the JSON file at `filepath` and unmarshals it into a Config struct.
func LoadConfig(filepath string) (*Config, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	var cfg Config
	if err := json.NewDecoder(file).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to decode JSON config: %w", err)
	}
	return &cfg, nil
}

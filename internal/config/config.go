package config

import (
	"encoding/json"
	"fmt"
	"log" 
	"os"
)

type PayloadField struct {
	Key    string  `json:"key"`    
	Type   string  `json:"type"`   
	Place  [2]int  `json:"place"`  
	Factor float64 `json:"factor"` 
}

type Conversion struct {
	Topic   string         `json:"topic"`  
	CanID   string         `json:"canid"`   
	Length  int            `json:"length"`  
	Payload []PayloadField `json:"payload"` 
}

type Config struct {
	Can2mqtt []Conversion `json:"can2mqtt"` 
	Mqtt2can []Conversion `json:"mqtt2can"` 
}

func LoadConfig(filepath string) (*Config, error) {
	log.Printf("Loading configuration from: %s", filepath) 
	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file '%s': %w", filepath, err)
	}
	defer file.Close()

	var cfg Config
	
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		
		return nil, fmt.Errorf("failed to decode JSON config from '%s': %w", filepath, err)
	}

	
	if len(cfg.Can2mqtt) == 0 && len(cfg.Mqtt2can) == 0 {
		log.Printf("Warning: Configuration file '%s' loaded successfully but contains no can2mqtt or mqtt2can rules.", filepath)
	} else {
		log.Printf("Config loaded successfully: %d can2mqtt, %d mqtt2can rules.", len(cfg.Can2mqtt), len(cfg.Mqtt2can))
	}

	return &cfg, nil
}

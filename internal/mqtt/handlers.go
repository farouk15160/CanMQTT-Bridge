package mqtt

import (
	"encoding/json"
	"log"
	"net"
)

// PublishStartInfo publishes a retained message indicating the translator is running.
func PublishStartInfo(c *Client) { // Expects *mqtt.Client
	startMessage := "CAN-MQTT Translator is up and running"
	ip := getIPAddress() // Get local IP

	payloadMap := map[string]string{
		"message":    startMessage,
		"ip_address": ip,
	}

	jsonBytes, err := json.Marshal(payloadMap)
	if err != nil {
		log.Printf("Error marshaling start info JSON: %v", err)
		return
	}
	jsonPayload := string(jsonBytes)

	// Use the retained publish function from the client
	if err := c.PublishRetained("translater/start", jsonPayload); err != nil { // Changed topic slightly
		log.Printf("Error publishing retained status to 'translater/start': %v", err)
	} else {
		log.Printf("Published retained message to 'translater/start': %s", jsonPayload)
	}
}

// getIPAddress tries to find the primary local IPv4 address.

func getIPAddress() string {
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, address := range addrs {
			if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					return ipnet.IP.String()
				}
			}
		}
	} else {
		log.Printf("Error getting interface addresses: %v", err)
	}
	log.Println("Warning: Could not determine local IP address.")
	return "unknown"
}

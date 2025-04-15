package mqtt

import (
	"encoding/json"
	"log"
	"net"
	"os/exec"
	"strings"
)

// PublishStartInfo publishes a retained message indicating the translator is running.
func PublishStartInfo(c *Client) { // Expects *mqtt.Client
	startMessage := "CAN-MQTT Translator is up and running"
	ip := getIPAddress() // Get local IP

	payloadMap := map[string]string{
		"start":      startMessage,
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
	// Method 1: Use Go's standard library (more portable)
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, address := range addrs {
			// Check the address type and if it is not a loopback
			if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					// Found an IPv4 address
					return ipnet.IP.String()
				}
			}
		}
	} else {
		log.Printf("Error getting interface addresses: %v", err)
	}

	// Method 2: Fallback to hostname -I (less portable)
	out, err := exec.Command("hostname", "-I").Output()
	if err == nil {
		// Split the output on whitespace to get each address
		allAddrs := strings.Fields(string(out))
		// Loop through all addresses, looking for the first IPv4
		for _, addrStr := range allAddrs {
			parsedIP := net.ParseIP(addrStr)
			// If we have a valid IPv4 address that's not loopback
			if parsedIP != nil && !parsedIP.IsLoopback() && parsedIP.To4() != nil {
				return addrStr
			}
		}
	} else {
		log.Printf("Error running 'hostname -I': %v", err)
	}

	// If no suitable address found
	log.Println("Warning: Could not determine local IP address.")
	return "unknown"
}

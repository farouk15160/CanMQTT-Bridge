package mqtt

import (
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"strconv"
	"time" 

	bridge "github.com/farouk15160/Translater-code-new/internal/bridge"
	config "github.com/farouk15160/Translater-code-new/internal/config"
)

func handleConfigUpdate(payload string) {
	log.Printf("[handleConfigUpdate] Received config update on 'translater/run': %s", payload)

	var cfgPayload ConfigPayload
	err := json.Unmarshal([]byte(payload), &cfgPayload)
	if err != nil {
		log.Printf("Error unmarshalling JSON payload for config update: %v", err)
		return 
	}

	log.Println("Applying configuration changes...")
	if cfgPayload.Debug != nil {
		bridge.SetDbg(*cfgPayload.Debug) 
	}
	if cfgPayload.Direction != nil {
		dirStr := strconv.Itoa(*cfgPayload.Direction)
		bridge.SetConfDirMode(dirStr)
	}
	if cfgPayload.File != nil {
		bridge.SetC2mf(*cfgPayload.File) 
	}
	if cfgPayload.Username != nil {
		bridge.SetUserName(*cfgPayload.Username) 
	}
	if cfgPayload.SleepTime != nil {
		sleepStr := strconv.FormatInt(*cfgPayload.SleepTime, 10)
		bridge.SetTimeSleepValue(sleepStr)
	}
	log.Println("[handleConfigUpdate] Finished processing config update.")
}

func handleTranslatorStatus(requestTopic string, payload string, mqttClient *Client) {
	log.Printf("[handleTranslatorStatus] Received request on topic '%s', gathering status...", requestTopic)
	_ = payload             
	startTime := time.Now() 

	var status TranslatorStatus
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	status.RAMUsage = m.Alloc            
	status.BufferUsage = getBufferUsage() 
	status.CPUUsageCores = getCPUUsage()  
	status.Temperature = getTemperature() 
	status.Uptime = getUptime()           
	gatherTime := time.Now()              

	readableStatus := ReadableTranslatorStatus{
		BufferUsage:   status.BufferUsage,
		CPUUsageCores: make(map[string]string),
		Temperature:   fmt.Sprintf("%.1f°C", status.Temperature),
		Uptime:        formatUptime(status.Uptime),
	}
	if status.Temperature < 0 {
		readableStatus.Temperature = "N/A"
	}

	totalMemory := getTotalMemory()
	if totalMemory > 0 {
		ramUsagePercent := (float64(status.RAMUsage) / float64(totalMemory)) * 100
		readableStatus.RAMUsage = fmt.Sprintf("%.2f%% (%d MB)", ramUsagePercent, status.RAMUsage/(1024*1024))
	} else {
		readableStatus.RAMUsage = fmt.Sprintf("%d MB (Total unknown)", status.RAMUsage/(1024*1024))
	}

	if len(status.CPUUsageCores) > 0 {
		readableStatus.CPUUsageCores["overall"] = fmt.Sprintf("%.2f%%", status.CPUUsageCores[0]*100)
	} else {
		readableStatus.CPUUsageCores["overall"] = "N/A"
	}

	jsonBytes, err := json.Marshal(readableStatus) 
	if err != nil {
		log.Printf("Error marshalling readable status to JSON: %v", err)
		return
	}
	payloadString := string(jsonBytes)
	marshalTime := time.Now() 

	statusTopic := "translater/status"
	if err := mqttClient.PublishRetained(statusTopic, payloadString); err != nil {
		
	} else {
		log.Printf("Initiated retained status update publish to '%s'", statusTopic)
	}
	publishInitiateTime := time.Now() 

	if mqttClient.debug { 
		log.Printf("[Perf Detail] Status Handler Timing: Gather=%v, Marshal=%v, PublishInit=%v, Total=%v",
			gatherTime.Sub(startTime),
			marshalTime.Sub(gatherTime),
			publishInitiateTime.Sub(marshalTime),
			time.Since(startTime))
	}
}

func PublishStartInfo(c *Client) { 
	startTopic := "translater/start"
	startMessage := "CAN-MQTT Translator is up and running"
	ip := getIPAddress()

	uname := "unknown"
	if config.UsernameFlag != nil && *config.UsernameFlag != "" {
		uname = *config.UsernameFlag
	}

	payloadMap := map[string]string{
		"message":    startMessage,
		"ip_address": ip,
		"username":   uname,
		"timestamp":  strconv.FormatInt(time.Now().Unix(), 10),
	}

	jsonBytes, err := json.MarshalIndent(payloadMap, "", "  ")
	if err != nil {
		log.Printf("Error marshaling start info JSON: %v", err)
		return
	}
	jsonPayload := string(jsonBytes)

	if err := c.PublishRetained(startTopic, jsonPayload); err != nil {
	
	} else {
		log.Printf("Initiated retained start info publish to '%s'", startTopic)
	}
}

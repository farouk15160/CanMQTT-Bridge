package mqtt

type ConfigPayload struct {
	Debug     *bool   `json:"debug"`
	Direction *int    `json:"direction"` 
	File      *string `json:"file"`
	Username  *string `json:"username"`
	SleepTime *int64  `json:"sleepTime"` 
	BitSize   *int    `json:"bit_size"`  
}

func dispatchMessage(topic, payload string, mqttClient *Client) bool {
	if topic == "translater/run" {
		handleConfigUpdate(payload)
		return true 
	}
	if topic == "translater/process" {
		handleTranslatorStatus(topic, payload, mqttClient)
		return true 
	}
	return false 
}

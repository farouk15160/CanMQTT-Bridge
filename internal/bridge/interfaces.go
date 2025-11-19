package bridge

type Publisher interface {
	Publish(topic, payload string) error         // CAN->MQTT data
	PublishRetained(topic, payload string) error //  status, start info
}

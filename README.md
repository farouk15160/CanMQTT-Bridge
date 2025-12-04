# 🌉 CanMQTT-Bridge

High-Performance, Bi-Directional Gateway between **CAN Bus** and **MQTT**.

---

## 📖 Overview

**CanMQTT-Bridge** is a production-ready Go application designed to bridge low-level CAN (Controller Area Network) hardware with high-level MQTT IoT networks.

It lets you control motors, read sensors, and monitor robots/vehicles using simple JSON over MQTT.

The bridge handles **all binary translation**, **scaling**, and **protocol conversion** automatically.

---

## 🏗️ System Architecture

The bridge is optimized for **high throughput** and **low latency** using Go concurrency.

### ✔️ Worker Pool Design

* Dedicated CAN workers
* Dedicated MQTT workers
* Prevents blocking & CPU starvation

### ✔️ SocketCAN Integration

* Direct Linux kernel CAN interface (e.g., `can0`)

### ✔️ Dynamic Translation

* JSON configuration maps binary CAN frames → readable JSON

### ✔️ Filtering

* Only subscribes to CAN IDs defined in config
* Reduces CPU usage

---

## ✨ Key Features

### 1. 🔄 Bi-Directional Translation

#### CAN → MQTT

* Reads CAN frames
* Converts binary data to JSON
* Applies scaling factors
* Publishes to MQTT topic

#### MQTT → CAN

* Receives JSON commands
* Converts scaled values back to raw binary
* Sends CAN frames to bus

---

### 2. 🎛️ Hot-Reload & Runtime Control

Via MQTT topic: `translater/run`

Controlled at runtime:

* Reload config instantly
* Change debug level
* Set artificial sleep time for throttling

---

### 3. ⏱️ CAN Bus Synchronization (Heartbeat)

Bridge acts as **time master**.

* CAN ID: `0x5`
* Frequency: 100ms
* Sends UNIX timestamp (nanoseconds)

Used for: microcontroller time sync.

---

## 4. 🏥 System Health Monitoring

Publishes telemetry to `translater/status`:

* CPU Load
* RAM Usage
* CPU Temperature
* Uptime

Useful for Raspberry Pi deployments.

---

## ⚙️ Configuration (messages_config.json)

Defines how CAN frames map to JSON.

### Example Item

```json
{
  "topic": "motor/data",
  "canid": "0x63",
  "length": 8,
  "payload": [
    { "key": "rpm", "type": "int16_t", "place": [2, 4], "factor": 1.0 },
    { "key": "torque", "type": "float", "place": [4, 8], "factor": 0.001 }
  ]
}
```

### Process Example

Incoming CAN (ID 0x63):

```
[0x00, 0x00, 0xE8, 0x03, 0x00, 0x40, 0x1C, 0x46]
```

Extracted:

* RPM = 1000
* Torque = 10.0

Outgoing MQTT:

```json
{"rpm": 1000, "torque": 10.0}
```

---

## 📡 Internal MQTT System Topics

### 1. `translater/status`

Direction: Bridge → MQTT

Provides:

* cpu_load
* ram_usage
* cpu_temp
* uptime

### 2. `translater/run`

Direction: MQTT → Bridge

Commands:

* `reload: true`
* `debug_level: 1-3`
* `sleep_time: ms`

### 3. CAN Clock Sync

CAN ID: `0x5`

---

## 👨‍💻 Interaction Examples

### Reload config & enable debug

```
mosquitto_pub -t "translater/run" -m '{"reload": true, "debug_level": 3}'
```

### Send control data to CAN

```
mosquitto_pub -t "motor/data" -m '{"rpm": 500, "torque": 2.5}'
```

### Monitor health

```
mosquitto_sub -t "translater/status"
```

---

## 🚀 Installation

### Prerequisites

* Go 1.18+
* SocketCAN (`can0`)
* MQTT broker

### Build

```
cd CanMQTT-Bridge
go mod tidy
go build -o can2mqtt cmd/can2mqtt/main.go
```

### Bring up CAN bus

```
sudo ip link set can0 up type can bitrate 500000
```

### Run

```
./can2mqtt
```

---

## 🛠️ Development Internals

* `cmd/can2mqtt/main.go` – Entry point
* `internal/bridge/convertfunctions.go` – Translation engine
* `internal/bridge/canbushandling.go` – CAN handling
* `internal/bridge/receivehandling.go` – Worker pools

---

## 📜 License

Distributed under the **MIT License**.

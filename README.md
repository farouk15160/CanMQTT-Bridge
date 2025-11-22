# CAN-MQTT Bridge

A high-performance, bidirectional Go application that bridges a CAN (Controller Area Network) bus and an MQTT broker. It allows for real-time translation of CAN frames into MQTT messages and vice-versa based on a dynamic JSON configuration.

## Features

* **Bidirectional Translation**: Supports CAN to MQTT, MQTT to CAN, or both simultaneously.
* **Dynamic Configuration**: Reload translation rules on-the-fly via MQTT without restarting the service.
* **Status Monitoring**: Reports system health (RAM, CPU, Buffer usage, Temperature) via MQTT.
* **Rate Limiting**: Configurable sleep time between CAN frame processing.
* **Internal Clock**: Built-in CAN clock generator for bus synchronization.
* **Concurrency**: Optimized to use all available CPU cores.

## Prerequisites

* **Go**: Version 1.18 or higher.
* **CAN Interface**: A configured SocketCAN interface (e.g., `can0`, `vcan0`).
* **MQTT Broker**: A running MQTT broker (e.g., Mosquitto).

## Installation & Compilation

1.  **Clone the repository:**
    git clone https://github.com/farouk15160/CanMQTT-Bridge
    cd CanMQTT-Bridge

2.  **Build the binary:**
    go build -o can2mqtt_bridge cmd/can2mqtt/main.go

3.  **Run the bridge:**
    ./can2mqtt_bridge

## Command-Line Usage

The application can be customized using the following flags:

| Flag | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `-v` | bool | `true` | Enable verbose debug output. |
| `-c` | string | `can0` | The CAN interface name to bind to. |
| `-m` | string | `...:1883` | MQTT broker URL (e.g., `tcp://localhost:1883`). |
| `-u` | string | `farouk` | MQTT Username (overrides URL username). |
| `-id`| string | `translater-client` | MQTT Client ID. |
| `-f` | string | `configs/messages_config.json` | Path to the translation configuration file. |
| `-d` | int | `0` | Direction: `0`=Bidirectional, `1`=CAN->MQTT, `2`=MQTT->CAN. |
| `-t` | int | `0` | Sleep time in microseconds (rate limiter). |
| `-T` | bool | `false` | Run CAN handling in a dedicated thread. |

**Example:**
./can2mqtt_bridge -c can1 -m "tcp://192.168.1.50:1883" -f ./my_config.json -d 0

## Configuration File Format

The mapping between CAN IDs and MQTT topics is defined in a JSON file. The file must contain two arrays: `can2mqtt` and `mqtt2can`.

### Structure

{
  "can2mqtt": [ ... ],
  "mqtt2can": [ ... ]
}

### Message Object Definition

Each message object defines how to parse the raw bytes.

* **topic**: The MQTT topic string.
* **canid**: The CAN ID (hex string, e.g., "0x123").
* **length**: DLC (Data Length Code) of the CAN frame (usually 8).
* **payload**: An array of fields to decode/encode.

#### Payload Field Definition
* **key**: The JSON key name for the MQTT payload.
* **type**: Data type (`uint8_t`, `int16_t`, `int32_t`, `float`, `unixtime`, `string`).
* **place**: `[StartByte, EndByte]`. The byte range in the CAN frame (0-indexed).
* **factor**: Multiplier for the value (used for scaling, e.g., converting raw integers to floats).

### Example Configuration

{
    "can2mqtt": [
        {
            "topic": "motor/data",
            "canid": "0x63",
            "length": 8,
            "payload": [
                {
                    "key": "rpm",
                    "type": "int16_t",
                    "place": [2, 4], 
                    "factor": 1.0
                },
                {
                    "key": "torque",
                    "type": "float",
                    "place": [4, 8],
                    "factor": 0.001
                }
            ]
        }
    ]
}

## MQTT Control Interface

The bridge subscribes to specific `translater/*` topics to allow remote management.

### 1. Status (`translater/status`)
* **Direction**: Bridge -> Broker (Retained)
* **Payload**: JSON object containing RAM usage, CPU usage, Uptime, and Buffer health.
* **Trigger**: Publishes periodically or upon request via `translater/process`.

### 2. Command: Reload/Configure (`translater/run`)
* **Direction**: Broker -> Bridge
* **Purpose**: Dynamically update configuration without restarting.
* **Payload**:
    {
      "file": "configs/new_config.json",
      "direction": 0,
      "debug": false,
      "sleepTime": 1000
    }

### 3. Command: Request Status (`translater/process`)
* **Direction**: Broker -> Bridge
* **Payload**: Any string.
* **Action**: Forces the bridge to publish immediate stats to `translater/status`.

### 4. Command: Clock Settings (`translater/clock`)
* **Direction**: Broker -> Bridge
* **Payload**: `{"takt": 20}`
* **Action**: Sets the frequency (Hz) of the internal heartbeat CAN message (ID `0x5`).

## Internal CAN Clock
The bridge automatically broadcasts a heartbeat on **CAN ID 0x5**.
* **Payload**: Unix timestamp (nanoseconds, LittleEndian).
* **Default Frequency**: 10Hz.
### Clock Accuracy
To get the Best Time accuracy, set the device running the bridge as your NTP-Server on your Network

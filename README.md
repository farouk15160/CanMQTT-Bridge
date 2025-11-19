# Translater-code-new

## Overview

Translater-code-new is a Go application that acts as a bridge between CAN (Controller Area Network) buses and MQTT (Message Queuing Telemetry Transport). It can convert CAN frames to MQTT messages and vice-versa, based on a JSON configuration file. It also provides an MQTT interface for dynamic configuration and status monitoring.

## Prerequisites

* **Go**: Version 1.18 or higher (ensure your GOPATH and GOROOT are set up correctly).
* **CAN interface**: A configured CAN interface on your system (e.g., `can0`).
* **MQTT Broker**: Access to an MQTT broker.

## Compilation

To compile the project, navigate to the root directory of the `Translater-code-new` project and run the following command:

```bash
go build -o can2mqtt_translator cmd/can2mqtt/main.go

Running the Translator
Execute the compiled binary from the project's root directory:
./can2mqtt_translator [flags]

Command-Line Flags:
The application behavior can be customized using the following command-line flags:

-v: (Boolean, default: true) Enable verbose/debug output.
Example: ./can2mqtt_translator -v=false
-c <interface_name>: (String, default: "can0") CAN interface name.
Example: ./can2mqtt_translator -c can1
-m <broker_url>: (String, default: "192.168.30.5:1883") MQTT broker connection string.
Format: [tcp://][user:pass@]host:port
Example: ./can2mqtt_translator -m "tcp://mymqttbroker.com:1883"
Example with auth: ./can2mqtt_translator -m "tcp://user:password@localhost:1883"
-f <file_path>: (String, default: "configs/messages_config.json") Path to the message conversion configuration JSON file.
Example: ./can2mqtt_translator -f "/path/to/your/custom_config.json"
-d <mode>: (Integer, default: 0) Direction mode for message translation.
0: Bidirectional (CAN <-> MQTT)
1: CAN to MQTT only
2: MQTT to CAN only
Example: ./can2mqtt_translator -d 1
-u <username>: (String, default: "farouk") MQTT username. This overrides any username specified in the broker URL if provided separately.
Example: ./can2mqtt_translator -u myuser
-t <microseconds>: (Integer, default: 0) Time to sleep in microseconds between processing CAN frames (can act as a rate limiter for CAN->MQTT).
Example: ./can2mqtt_translator -t 1000 (sleeps for 1ms)
-T: (Boolean, default: false) Run CAN handling in a separate thread.
Example: ./can2mqtt_translator -T=true
-id <client_id>: (String, default: "translater-client") MQTT client ID.
Example: ./can2mqtt_translator -id myCanTranslator

Configuration File
The translator uses a JSON file (specified by the -f flag, defaulting to configs/messages_config.json) to define rules for converting messages between CAN and MQTT. This file contains two main arrays: can2mqtt and mqtt2can, each with objects defining topics, CAN IDs, payload structures, and conversion factors.

MQTT Interface (translater/* topics)
The application uses several MQTT topics prefixed with translater/ for control, configuration, and status updates.

1. translater/start (Retained)
Published by: Translator on startup.
Purpose: Indicates that the CAN-MQTT Translator is up and running.
Payload Example (JSON):
JSON

{
  "message": "CAN-MQTT Translator is up and running",
  "ip_address": "192.168.1.100",
  "username": "farouk",
  "timestamp": "1678886400"
}
2. translater/run
Published to: By an external client to the translator.
Purpose: To dynamically update the translator's configuration. If the file field is provided and changed, the message configuration rules will be reloaded from the new file.
Payload (JSON): ConfigPayload - all fields are optional.
JSON
{
  "debug": true,
  "direction": 0,
  "file": "configs/new_messages_config.json",
  "username": "new_user",
  "sleepTime": 500,
  "bit_size": 64
}
debug (boolean): Enable/disable debug logging.
direction (integer): Set direction mode (0: bidirectional, 1: CAN->MQTT, 2: MQTT->CAN).
file (string): Path to the message configuration JSON file. Changing this triggers a reload.
username (string): MQTT username.
sleepTime (integer): Sleep time in microseconds between CAN frames.
bit_size (integer): Specifies the data length for outgoing CAN frames when converting MQTT to CAN. Valid inputs are 8, 16, 32, 64, corresponding to 1, 2, 4, or 8 bytes for the CAN frame's Data Length Code (DLC).
3. translater/process
Published to: By an external client to the translator.
Purpose: Request the current operational status of the translator.
Payload: Any string (the content of the payload is currently ignored).
Response: The translator publishes its status to the translater/status topic.
4. translater/status (Retained)
Published by: Translator in response to a message on translater/process.
Purpose: Provides current operational status and system metrics of the translator.
Payload Example (JSON): ReadableTranslatorStatus
JSON

{
  "ram_usage": "10.52% (168 MB)",
  "buffer_usage": {
    "UsedMB": 2048,
    "AvailableMB": 12288,
    "UsedPercent": 16.66
  },
  "cpu_usage_cores": {
    "overall": "25.75%"
  },
  "temperature": "45.5°C",
  "uptime": "3 days, 10 hours, 30 minutes, 5 seconds"
}
ram_usage: RAM usage by the translator process.
buffer_usage: System memory buffer/cache usage (from /proc/meminfo on Linux).
cpu_usage_cores: Overall CPU usage.
temperature: System temperature (if available).
uptime: System uptime.
5. translater/clock
Published to: By an external client to the translator.
Purpose: To update the frequency of the internal CAN clock sender.
Payload (JSON): ClockConfigPayload - takt is optional.
JSON

{
  "takt": 20
}
takt (integer): Desired frequency in Hz for the CAN clock messages (CAN ID 0x5). Default is 10 Hz.
Internal CAN Clock Sender
The translator includes a built-in CAN message sender that periodically transmits a "clock" message.

CAN ID: 0x5
CAN DLC (Length): 8 bytes
CAN Payload: Current Unix timestamp in nanoseconds (uint64, LittleEndian encoding).
Frequency: Configurable via the translater/clock MQTT topic (defaults to 10 Hz). This message is useful for timestamping or as a heartbeat on the CAN bus.

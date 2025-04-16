package bridge

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log" // Added for logging
	"math"
	"strconv"
	"strings"
	"time" // Import time for timestamp override

	// "sync" // Removed sync as clockMutex is removed

	"github.com/brutella/can"
	// Import config package for struct types
	"github.com/farouk15160/Translater-code-new/internal/config"
)

// Removed: var clockMutex sync.RWMutex
// Removed: var lastClock string = "00"

// Struct definition for MQTT response payload
type mqttResponse struct {
	Topic   string
	Payload string
}

// getPayloadConv searches the loaded configuration for matching rules.
// Uses the package variable 'loadedConfig'.
func getPayloadConv(id string, mode string) (*config.Conversion, error) {
	if loadedConfig == nil {
		return nil, fmt.Errorf("cannot get payload conversion: config not loaded")
	}

	var searchList []config.Conversion
	var isCanToMqtt bool

	switch mode {
	case "can2mqtt":
		searchList = loadedConfig.Can2mqtt
		isCanToMqtt = true
	case "mqtt2can":
		searchList = loadedConfig.Mqtt2can
		isCanToMqtt = false
	default:
		return nil, fmt.Errorf("invalid mode '%s' passed to getPayloadConv", mode)
	}

	for i := range searchList {
		conv := &searchList[i] // Get pointer to avoid copying large struct
		var compareID string
		if isCanToMqtt {
			// Compare CAN ID (hex string format)
			compareID = conv.CanID
			// Normalize input ID for comparison (ensure 0x prefix, uppercase?)
			if !strings.HasPrefix(id, "0x") {
				parsedID, err := strconv.ParseUint(id, 16, 32)
				if err != nil {
					continue
				}
				id = fmt.Sprintf("0x%X", parsedID)
			}
			// Normalize config ID
			if !strings.HasPrefix(compareID, "0x") {
				parsedID, err := strconv.ParseUint(compareID, 16, 32)
				if err == nil {
					compareID = fmt.Sprintf("0x%X", parsedID)
				}
			}

		} else {
			// Compare MQTT Topic
			compareID = conv.Topic
		}

		if compareID == id {
			if debugMode {
				log.Printf("getPayloadConv: Found matching conversion for ID/Topic '%s' (mode: %s)", id, mode)
			}
			return conv, nil // Return pointer to the found conversion rule and nil error
		}
	}

	// No match found
	return nil, fmt.Errorf("no matching conversion rule found for %s '%s'", mode, id)
}

// convert2MQTT converts a CAN frame to an MQTT topic and JSON payload string.
// It now ALWAYS adds/overwrites a "timestamp" field with the translator's current time.
// Returns mqttResponse and error.
func convert2MQTT(id int, length int, payload [8]byte) (mqttResponse, error) {
	res := mqttResponse{}            // Initialize empty response
	idStr := fmt.Sprintf("0x%X", id) // Format ID as hex string (e.g., 0x1A)

	convRule, err := getPayloadConv(idStr, "can2mqtt")
	if err != nil {
		res.Topic = "error/conversion"
		res.Payload = fmt.Sprintf(`{"error":"no matching rule for CAN ID %s"}`, idStr)
		return res, err // Return the error from getPayloadConv
	}

	res.Topic = convRule.Topic // Set the correct topic from the rule

	// --- Step 1: Create initial JSON map based on conversion rules ---
	jsonData := make(map[string]interface{})
	var processingError error = nil // Track errors during field processing

	for _, field := range convRule.Payload {
		var value interface{} // Use interface{} to hold various types
		var fieldErr error = nil

		startIndex := field.Place[0]
		endIndex := field.Place[1]

		if startIndex < 0 || startIndex >= 8 || endIndex <= startIndex || endIndex > 8 {
			log.Printf("convert2MQTT Warning: Invalid place %v for field '%s' in CAN ID %s rule. Skipping field.", field.Place, field.Key, idStr)
			continue
		}
		if startIndex >= length || endIndex > length {
			log.Printf("convert2MQTT Warning: Place %v for field '%s' (CAN ID %s) exceeds payload length %d. Skipping field.", field.Place, field.Key, idStr, length)
			continue
		}

		subSlice := payload[startIndex:endIndex]

		switch field.Type {
		case "error":
			value = "error" // Store as string
		case "unixtime": // If config still uses "unixtime", convert it but it will be overwritten later
			if len(subSlice) >= 4 {
				unixSec := binary.LittleEndian.Uint32(subSlice[0:4])
				var unixMs uint32 = 0
				if len(subSlice) >= 8 {
					unixMs = binary.LittleEndian.Uint32(subSlice[4:8])
				}
				value = float64(unixSec) + float64(unixMs)/1000.0
			} else {
				fieldErr = fmt.Errorf("'unixtime' field '%s' needs at least 4 bytes, got %d", field.Key, len(subSlice))
			}
		case "byte", "string":
			value = string(subSlice) // Store as string
		case "int8_t":
			if len(subSlice) >= 1 {
				value = field.Factor * float64(int8(subSlice[0]))
			} else {
				fieldErr = fmt.Errorf("not enough bytes for int8_t")
			}
		case "uint8_t":
			if len(subSlice) >= 1 {
				value = field.Factor * float64(subSlice[0])
			} else {
				fieldErr = fmt.Errorf("not enough bytes for uint8_t")
			}
		case "int16_t":
			if len(subSlice) >= 2 {
				value = field.Factor * float64(int16(binary.LittleEndian.Uint16(subSlice)))
			} else {
				fieldErr = fmt.Errorf("not enough bytes for int16_t")
			}
		case "uint16_t":
			if len(subSlice) >= 2 {
				value = field.Factor * float64(binary.LittleEndian.Uint16(subSlice))
			} else {
				fieldErr = fmt.Errorf("not enough bytes for uint16_t")
			}
		case "int32_t":
			if len(subSlice) >= 4 {
				value = field.Factor * float64(int32(binary.LittleEndian.Uint32(subSlice)))
			} else {
				fieldErr = fmt.Errorf("not enough bytes for int32_t")
			}
		case "uint32_t":
			if len(subSlice) >= 4 {
				value = field.Factor * float64(binary.LittleEndian.Uint32(subSlice))
			} else {
				fieldErr = fmt.Errorf("not enough bytes for uint32_t")
			}
		case "float": // Assume float32
			if len(subSlice) >= 4 {
				value = field.Factor * float64(math.Float32frombits(binary.LittleEndian.Uint32(subSlice)))
			} else {
				fieldErr = fmt.Errorf("not enough bytes for float32")
			}
		default:
			fieldErr = fmt.Errorf("unknown field type '%s' for key '%s'", field.Type, field.Key)
		}

		if fieldErr != nil {
			log.Printf("convert2MQTT Warning: Error processing field '%s' in CAN ID %s: %v. Skipping.", field.Key, idStr, fieldErr)
			if processingError == nil {
				processingError = fieldErr
			} // Store first error
			continue
		}
		jsonData[field.Key] = value // Add successfully processed field to map
	} // End field loop

	// --- Step 2: Add/Overwrite timestamp with translator's current time ---
	// Use Unix float seconds (seconds since epoch with nanosecond precision)
	jsonData["timestamp"] = float64(time.Now().UnixNano()) / 1e9

	// --- Step 3: Marshal the final map to JSON ---
	jsonBytes, err := json.Marshal(jsonData)
	if err != nil {
		log.Printf("convert2MQTT Error: Failed to marshal final JSON for CAN ID %s: %v", idStr, err)
		res.Topic = "error/processing"
		res.Payload = fmt.Sprintf(`{"error":"failed to marshal JSON for CAN ID %s", "details":"%v"}`, idStr, err)
		// Return marshalling error, or the first field processing error if it exists
		if processingError != nil {
			return res, fmt.Errorf("field processing error (%v) and marshalling error (%v)", processingError, err)
		}
		return res, err
	}

	res.Payload = string(jsonBytes)

	if debugMode {
		logPayload := res.Payload
		if len(logPayload) > 100 {
			logPayload = logPayload[:100] + "..."
		}
		log.Printf("convert2MQTT: ID=%s -> Topic=%s, Payload=%s", idStr, res.Topic, logPayload)
	}

	// Return success (nil error) or the first field processing error encountered
	return res, processingError
}

// convert2CAN converts an MQTT topic and JSON payload []byte to a CAN frame.
// (No changes needed here for timestamp override, as that applies to CAN->MQTT)
// Returns can.Frame and error.
func convert2CAN(topic string, payload []byte) (can.Frame, error) {
	frame := can.Frame{} // Initialize empty frame

	convRule, err := getPayloadConv(topic, "mqtt2can")
	if err != nil {
		return frame, err // Return the error from getPayloadConv
	}

	var data map[string]interface{}
	err = json.Unmarshal(payload, &data)
	if err != nil {
		err = fmt.Errorf("failed to unmarshal JSON payload for topic %s: %w", topic, err)
		log.Printf("convert2CAN Error: %v", err)
		return frame, err // Return empty frame and JSON error
	}

	var buffer [8]uint8 // CAN frame data buffer

	for _, field := range convRule.Payload {
		value, keyExists := data[field.Key]
		if !keyExists {
			continue // Skip if key is missing in JSON
		}

		// Skip the timestamp field if present in MQTT->CAN rule, it's usually added by translator
		if field.Key == "timestamp" {
			continue
		}

		startIndex := field.Place[0]
		endIndex := field.Place[1]

		if startIndex < 0 || startIndex >= 8 || endIndex <= startIndex || endIndex > 8 {
			log.Printf("convert2CAN Warning: Invalid place %v for field '%s' in topic %s rule. Skipping field.", field.Place, field.Key, topic)
			continue
		}

		var convErr error = nil

		switch field.Type {
		case "byte", "string":
			strVal, ok := value.(string)
			if !ok {
				convErr = fmt.Errorf("expected string for key '%s', got %T", field.Key, value)
				break
			}
			byteVal := []byte(strVal)
			count := copy(buffer[startIndex:endIndex], byteVal)
			if count < len(byteVal) && debugMode {
				log.Printf("convert2CAN Warning: String value for key '%s' truncated for topic %s.", field.Key, topic)
			}
		case "unixtime":
			continue // Ignore unixtime field in MQTT->CAN rules
		default: // Handle numeric types
			numVal, ok := value.(float64)
			if !ok {
				// Allow integers directly? json unmarshals numbers to float64 by default
				// Check if it's an integer type if float64 fails
				switch v := value.(type) {
				case int:
					numVal = float64(v)
				case int8:
					numVal = float64(v)
				case int16:
					numVal = float64(v)
				case int32:
					numVal = float64(v)
				case int64:
					numVal = float64(v)
				case uint:
					numVal = float64(v)
				case uint8:
					numVal = float64(v)
				case uint16:
					numVal = float64(v)
				case uint32:
					numVal = float64(v)
				case uint64:
					numVal = float64(v)
				default:
					convErr = fmt.Errorf("expected number for key '%s', got %T", field.Key, value)
					break // Break inner switch
				}
				if convErr != nil {
					break
				} // Break outer switch if type error
			}

			valToConvert := numVal
			if field.Factor != 0 {
				valToConvert = numVal / field.Factor
			} else if numVal != 0 {
				convErr = fmt.Errorf("zero factor for non-zero value '%f' for key '%s'", numVal, field.Key)
				break
			}

			switch field.Type {
			case "int8_t":
				if endIndex != startIndex+1 {
					convErr = fmt.Errorf("place %v invalid for int8_t", field.Place)
					break
				}
				if valToConvert < math.MinInt8 || valToConvert > math.MaxInt8 {
					convErr = fmt.Errorf("value %f out of range for int8_t", valToConvert)
					break
				}
				buffer[startIndex] = byte(int8(valToConvert))
			case "uint8_t":
				if endIndex != startIndex+1 {
					convErr = fmt.Errorf("place %v invalid for uint8_t", field.Place)
					break
				}
				if valToConvert < 0 || valToConvert > math.MaxUint8 {
					convErr = fmt.Errorf("value %f out of range for uint8_t", valToConvert)
					break
				}
				buffer[startIndex] = uint8(valToConvert)
			case "int16_t":
				if endIndex != startIndex+2 {
					convErr = fmt.Errorf("place %v invalid for int16_t", field.Place)
					break
				}
				if valToConvert < math.MinInt16 || valToConvert > math.MaxInt16 {
					convErr = fmt.Errorf("value %f out of range for int16_t", valToConvert)
					break
				}
				binary.LittleEndian.PutUint16(buffer[startIndex:endIndex], uint16(int16(valToConvert)))
			case "uint16_t":
				if endIndex != startIndex+2 {
					convErr = fmt.Errorf("place %v invalid for uint16_t", field.Place)
					break
				}
				if valToConvert < 0 || valToConvert > math.MaxUint16 {
					convErr = fmt.Errorf("value %f out of range for uint16_t", valToConvert)
					break
				}
				binary.LittleEndian.PutUint16(buffer[startIndex:endIndex], uint16(valToConvert))
			case "int32_t":
				if endIndex != startIndex+4 {
					convErr = fmt.Errorf("place %v invalid for int32_t", field.Place)
					break
				}
				if valToConvert < math.MinInt32 || valToConvert > math.MaxInt32 {
					convErr = fmt.Errorf("value %f out of range for int32_t", valToConvert)
					break
				}
				binary.LittleEndian.PutUint32(buffer[startIndex:endIndex], uint32(int32(valToConvert)))
			case "uint32_t":
				if endIndex != startIndex+4 {
					convErr = fmt.Errorf("place %v invalid for uint32_t", field.Place)
					break
				}
				if valToConvert < 0 || valToConvert > math.MaxUint32 {
					convErr = fmt.Errorf("value %f out of range for uint32_t", valToConvert)
					break
				}
				binary.LittleEndian.PutUint32(buffer[startIndex:endIndex], uint32(valToConvert))
			case "float": // Assume float32
				if endIndex != startIndex+4 {
					convErr = fmt.Errorf("place %v invalid for float32", field.Place)
					break
				}
				binary.LittleEndian.PutUint32(buffer[startIndex:endIndex], math.Float32bits(float32(valToConvert)))
			default:
				convErr = fmt.Errorf("unknown numeric field type '%s' for key '%s'", field.Type, field.Key)
			}
		} // End type switch

		if convErr != nil {
			log.Printf("convert2CAN Warning: Conversion error for key '%s' in topic %s: %v. Skipping field.", field.Key, topic, convErr)
			// Optionally return error immediately: return frame, convErr
		}
	} // End field loop

	canidStr := strings.TrimPrefix(convRule.CanID, "0x")
	canidNr, err := strconv.ParseUint(canidStr, 16, 32)
	if err != nil {
		err = fmt.Errorf("failed to parse CAN ID '%s' for topic %s: %w", convRule.CanID, topic, err)
		log.Printf("convert2CAN Error: %v", err)
		return frame, err // Return empty frame and ID parse error
	}

	frame.ID = uint32(canidNr)
	frame.Length = uint8(convRule.Length)
	if frame.Length > 8 {
		log.Printf("convert2CAN Warning: Configured length %d for CAN ID %X exceeds 8 bytes. Clamping to 8.", convRule.Length, frame.ID)
		frame.Length = 8
	}
	frame.Data = buffer

	if debugMode {
		log.Printf("convert2CAN: Topic=%s -> CAN Frame: ID=%X Len=%d Data=%X", topic, frame.ID, frame.Length, frame.Data[:frame.Length])
	}
	return frame, nil // Return successful frame and nil error
}

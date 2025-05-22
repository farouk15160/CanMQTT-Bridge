package bridge

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log" // Keep log import
	"math"
	"strconv" // Keep for CAN ID parsing during map creation
	"strings" // Keep for CAN ID parsing
	"time"    // Keep for timestamping

	"github.com/brutella/can"
	"github.com/farouk15160/Translater-code-new/internal/config" // Keep for types
)

// Struct definition for MQTT response payload
type mqttResponse struct {
	Topic   string
	Payload string
}

// Convert2MQTTUsingMap converts a CAN frame using the preprocessed CanRuleMap.
// It adds a 'unixtime' field with the current Unix timestamp in NANOSECONDS to the MQTT payload.
// Returns mqttResponse, the matching *config.Conversion rule, and error.
// --- EXPORTED ---
func Convert2MQTTUsingMap(id uint32, length int, payload [8]byte) (mqttResponse, *config.Conversion, error) {
	res := mqttResponse{}

	ConfigLock.RLock()                 // Use exported name - Read lock for accessing the map
	convRule, exists := CanRuleMap[id] // Use exported name
	currentDebugMode := debugMode      // Read debug flag under lock
	ConfigLock.RUnlock()               // Use exported name - Release lock

	if !exists {
		return res, nil, fmt.Errorf("no matching conversion rule found")
	}

	res.Topic = convRule.Topic
	jsonData := make(map[string]interface{})
	var processingError error = nil

	for _, field := range convRule.Payload {
		var value interface{}
		var fieldErr error = nil

		if field.Type == "unixtime" {
			continue
		}

		startIndex := field.Place[0]
		endIndex := field.Place[1]

		if startIndex < 0 || startIndex >= 8 || endIndex <= startIndex || endIndex > 8 {
			if currentDebugMode {
				log.Printf("Convert2MQTT Warning: Invalid place %v for field '%s' in CAN ID %X rule. Skipping field.", field.Place, field.Key, id)
			}
			continue
		}
		if startIndex >= length {
			continue
		}
		if endIndex > length {
			endIndex = length
		}
		if startIndex >= endIndex {
			if currentDebugMode {
				log.Printf("Convert2MQTT Warning: Invalid adjusted place [%d:%d) for field '%s' (CAN ID %X) with payload length %d. Skipping field.", startIndex, endIndex, field.Key, id, length)
			}
			continue
		}

		subSlice := payload[startIndex:endIndex]

		switch field.Type {
		case "error":
			value = "error"
		case "byte", "string":
			value = string(subSlice)
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
			log.Printf("Convert2MQTT Warning: Error processing field '%s' in CAN ID %X: %v. Skipping.", field.Key, id, fieldErr)
			if processingError == nil {
				processingError = fieldErr
			}
			continue
		}
		jsonData[field.Key] = value
	} // End field loop

	// Add the current Unix timestamp in nanoseconds since epoch
	now := time.Now()
	seconds := now.Unix()
	microseconds := now.Nanosecond() / 1000

	last_clock := fmt.Sprintf("%d.%06d", seconds, microseconds)

	jsonData["unixtime"] = last_clock // Changed from Unix() to UnixNano()

	jsonBytes, err := json.Marshal(jsonData)
	if err != nil {
		log.Printf("Convert2MQTT Error: Failed to marshal final JSON for CAN ID %X: %v", id, err)
		res.Topic = "error/processing"
		res.Payload = fmt.Sprintf(`{"error":"failed to marshal JSON for CAN ID %X", "details":"%v"}`, id, err)
		if processingError != nil {
			return res, convRule, fmt.Errorf("field processing error (%v) and marshalling error (%v)", processingError, err)
		}
		return res, convRule, err
	}

	res.Payload = string(jsonBytes)

	// --- ADDED: Debug log for final MQTT payload ---
	if currentDebugMode {
		// Truncate payload for logging if it's too long
		logPayload := res.Payload
		if len(logPayload) > 200 { // Limit log length
			logPayload = logPayload[:200] + "..."
		}
		log.Printf("[Debug CAN->MQTT Payload] Sending to Topic '%s': %s", res.Topic, logPayload)
	}
	// --- END ADDED ---

	return res, convRule, processingError
}

// Convert2CANUsingMap converts an MQTT message using the preprocessed MqttRuleMap.
// Sets frame.Length based on the globally configured bit size (clamped to 8).
// Returns can.Frame, the matching *config.Conversion rule, and error.
// --- EXPORTED ---
func Convert2CANUsingMap(topic string, payload []byte) (can.Frame, *config.Conversion, error) {
	frame := can.Frame{} // Uses brutella/can Frame with Data [8]uint8

	ConfigLock.RLock()                     // Use exported name - Read lock for accessing the map
	convRule, exists := MqttRuleMap[topic] // Use exported name
	currentDebugMode := debugMode          // Read debug flag under lock
	configuredBytes := currentBitSize      // Read configured size (already clamped 1-8)
	ConfigLock.RUnlock()                   // Use exported name - Release lock

	if !exists {
		return frame, nil, fmt.Errorf("no matching conversion rule found")
	}

	var data map[string]interface{}
	err := json.Unmarshal(payload, &data)
	if err != nil {
		err = fmt.Errorf("failed to unmarshal JSON payload for topic %s: %w", topic, err)
		log.Printf("Convert2CAN Error: %v", err)
		return frame, convRule, err
	}

	var buffer [8]uint8 // Still limited to 8 bytes by the library's frame struct
	var fieldProcessingError error = nil

	for _, field := range convRule.Payload {
		value, keyExists := data[field.Key]
		if !keyExists {
			continue
		}

		if field.Key == "timestamp" || field.Type == "unixtime" || field.Key == "unixtime" {
			continue
		}

		startIndex := field.Place[0]
		endIndex := field.Place[1]

		if startIndex < 0 || startIndex >= 8 || endIndex <= startIndex || endIndex > 8 {
			if currentDebugMode {
				log.Printf("Convert2CAN Warning: Invalid place %v (must be within [0-8]) for field '%s' in topic %s rule. Skipping field.", field.Place, field.Key, topic)
			}
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
			if count < len(byteVal) && currentDebugMode {
				log.Printf("Convert2CAN Warning: String value for key '%s' truncated for topic %s (max %d bytes fit).", field.Key, topic, endIndex-startIndex)
			}
		default: // Handle numeric types
			numVal, ok := value.(float64)
			if !ok {
				switch v := value.(type) {
				case int:
					numVal = float64(v)
					ok = true
				case int8:
					numVal = float64(v)
					ok = true
				case int16:
					numVal = float64(v)
					ok = true
				case int32:
					numVal = float64(v)
					ok = true
				case int64:
					numVal = float64(v)
					ok = true
				case uint:
					numVal = float64(v)
					ok = true
				case uint8:
					numVal = float64(v)
					ok = true
				case uint16:
					numVal = float64(v)
					ok = true
				case uint32:
					numVal = float64(v)
					ok = true
				case uint64:
					numVal = float64(v)
					ok = true
				}
			}
			if !ok {
				convErr = fmt.Errorf("expected number for key '%s', got %T", field.Key, value)
				break
			}

			valToConvert := numVal
			if field.Factor != 0 {
				valToConvert = numVal / field.Factor
			} else if numVal != 0 {
				convErr = fmt.Errorf("zero factor for non-zero value '%f' for key '%s'", numVal, field.Key)
				break
			}

			var targetVal interface{}
			switch field.Type {
			case "int8_t":
				if valToConvert < math.MinInt8 || valToConvert > math.MaxInt8 {
					convErr = fmt.Errorf("value %f out of range for int8_t", valToConvert)
					break
				}
				targetVal = int8(valToConvert)
			case "uint8_t":
				if valToConvert < 0 || valToConvert > math.MaxUint8 {
					convErr = fmt.Errorf("value %f out of range for uint8_t", valToConvert)
					break
				}
				targetVal = uint8(valToConvert)
			case "int16_t":
				if valToConvert < math.MinInt16 || valToConvert > math.MaxInt16 {
					convErr = fmt.Errorf("value %f out of range for int16_t", valToConvert)
					break
				}
				targetVal = int16(valToConvert)
			case "uint16_t":
				if valToConvert < 0 || valToConvert > math.MaxUint16 {
					convErr = fmt.Errorf("value %f out of range for uint16_t", valToConvert)
					break
				}
				targetVal = uint16(valToConvert)
			case "int32_t":
				if valToConvert < math.MinInt32 || valToConvert > math.MaxInt32 {
					convErr = fmt.Errorf("value %f out of range for int32_t", valToConvert)
					break
				}
				targetVal = int32(valToConvert)
			case "uint32_t":
				if valToConvert < 0 || valToConvert > math.MaxUint32 {
					convErr = fmt.Errorf("value %f out of range for uint32_t", valToConvert)
					break
				}
				targetVal = uint32(valToConvert)
			case "float":
				targetVal = float32(valToConvert)
			default:
				convErr = fmt.Errorf("unknown numeric field type '%s' for key '%s'", field.Type, field.Key)
			}
			if convErr != nil {
				break
			}

			switch v := targetVal.(type) {
			case int8:
				if endIndex == startIndex+1 {
					buffer[startIndex] = byte(v)
				} else {
					convErr = fmt.Errorf("place %v invalid for int8_t", field.Place)
				}
			case uint8:
				if endIndex == startIndex+1 {
					buffer[startIndex] = v
				} else {
					convErr = fmt.Errorf("place %v invalid for uint8_t", field.Place)
				}
			case int16:
				if endIndex == startIndex+2 {
					binary.LittleEndian.PutUint16(buffer[startIndex:endIndex], uint16(v))
				} else {
					convErr = fmt.Errorf("place %v invalid for int16_t", field.Place)
				}
			case uint16:
				if endIndex == startIndex+2 {
					binary.LittleEndian.PutUint16(buffer[startIndex:endIndex], v)
				} else {
					convErr = fmt.Errorf("place %v invalid for uint16_t", field.Place)
				}
			case int32:
				if endIndex == startIndex+4 {
					binary.LittleEndian.PutUint32(buffer[startIndex:endIndex], uint32(v))
				} else {
					convErr = fmt.Errorf("place %v invalid for int32_t", field.Place)
				}
			case uint32:
				if endIndex == startIndex+4 {
					binary.LittleEndian.PutUint32(buffer[startIndex:endIndex], v)
				} else {
					convErr = fmt.Errorf("place %v invalid for uint32_t", field.Place)
				}
			case float32:
				if endIndex == startIndex+4 {
					binary.LittleEndian.PutUint32(buffer[startIndex:endIndex], math.Float32bits(v))
				} else {
					convErr = fmt.Errorf("place %v invalid for float32", field.Place)
				}
			}
		} // End type switch

		if convErr != nil {
			log.Printf("Convert2CAN Warning: Conversion error for key '%s' in topic %s: %v. Skipping field.", field.Key, topic, convErr)
			if fieldProcessingError == nil {
				fieldProcessingError = convErr
			}
		}
	} // End field loop

	canidStr := strings.TrimPrefix(convRule.CanID, "0x")
	canidNr, _ := strconv.ParseUint(canidStr, 16, 32)

	frame.ID = uint32(canidNr)
	// frame.Length = configuredBytes // Use the clamped value (1-8)
	frame.Length = uint8(configuredBytes) // Use the clamped value (1-8)
	frame.Data = buffer                   // Assign the 8-byte buffer

	return frame, convRule, fieldProcessingError
}

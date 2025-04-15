package bridge

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log" // Added for logging
	"math"
	"strconv"
	"strings"

	"github.com/brutella/can"
	// Import config package for struct types
	"github.com/farouk15160/Translater-code-new/internal/config"
)

// Struct definition for MQTT response payload (if needed elsewhere)
type mqttResponse struct {
	Topic   string
	Payload string
}

// getPayloadConv searches the loaded configuration for matching rules.
// Uses the package variable 'loadedConfig'.
func getPayloadConv(id string, mode string) (*config.Config, *config.Conversion) {
	if loadedConfig == nil {
		log.Println("Error: getPayloadConv called but config not loaded.")
		return nil, nil
	}

	var searchList []config.Conversion
	var isCanToMqtt bool

	if mode == "can2mqtt" {
		searchList = loadedConfig.Can2mqtt
		isCanToMqtt = true
	} else if mode == "mqtt2can" {
		searchList = loadedConfig.Mqtt2can
		isCanToMqtt = false
	} else {
		log.Printf("Error: Invalid mode '%s' passed to getPayloadConv.", mode)
		return nil, nil
	}

	for i := range searchList {
		conv := &searchList[i] // Get pointer to avoid copying large struct
		var compareID string
		if isCanToMqtt {
			compareID = conv.CanID
		} else {
			compareID = conv.Topic
		}

		if compareID == id {
			if debugMode {
				log.Printf("getPayloadConv: Found matching conversion for ID '%s' (mode: %s)", id, mode)
			}
			return loadedConfig, conv // Return pointer to the found conversion rule
		}
	}

	if debugMode {
		log.Printf("getPayloadConv: No matching conversion found for ID '%s' (mode: %s)", id, mode)
	}
	return nil, nil // Return nil if no match found
}

// convert2MQTT converts a CAN frame to an MQTT topic and JSON payload string.
func convert2MQTT(id int, length int, payload [8]byte) mqttResponse {
	res := mqttResponse{Topic: "error/conversion", Payload: `{"error":"no matching rule"}`} // Default error response
	idStr := fmt.Sprintf("0x%X", id)                                                        // Format ID as hex string (e.g., 0x1A)

	_, convRule := getPayloadConv(idStr, "can2mqtt")
	if convRule == nil {
		if debugMode {
			log.Printf("convert2MQTT: No rule found for CAN ID %s", idStr)
		}
		return res // Return default error if no rule
	}

	res.Topic = convRule.Topic // Set the correct topic from the rule

	// Use strings.Builder for efficient string construction
	var retstr strings.Builder
	retstr.WriteString("{")
	var fieldCount int // Track number of fields added

	for _, field := range convRule.Payload {
		var valStr string
		// Ensure place indices are valid for the payload length
		if field.Place[0] >= length || (field.Place[1] > 0 && field.Place[1] > length) || field.Place[0] < 0 || (field.Place[1] > 0 && field.Place[1] <= field.Place[0]) {
			log.Printf("convert2MQTT Warning: Invalid place %v for field '%s' in CAN ID %s (payload length %d). Skipping field.", field.Place, field.Key, idStr, length)
			continue
		}

		// Determine slice bounds carefully
		startIndex := field.Place[0]
		endIndex := field.Place[1]  // If Place[1] is 0 or same as Place[0], usually means single byte/bit range? Config needs clarity.
		if endIndex <= startIndex { // Assuming endIndex <= startIndex means single byte access at startIndex
			endIndex = startIndex + 1
		}
		if endIndex > length { // Ensure endIndex doesn't exceed actual payload length
			endIndex = length
			log.Printf("convert2MQTT Warning: Adjusted endIndex for field '%s' due to payload length.", field.Key)
		}
		if startIndex >= endIndex {
			log.Printf("convert2MQTT Warning: Invalid slice indices [%d:%d] for field '%s'. Skipping.", startIndex, endIndex, field.Key)
			continue
		}

		subSlice := payload[startIndex:endIndex]

		switch field.Type {
		case "error":
			valStr = `"error"` // JSON string "error"
		case "unixtime":
			if len(subSlice) >= 4 { // Need at least 4 bytes for seconds
				unixSec := binary.LittleEndian.Uint32(subSlice[0:4])
				var unixMs uint32 = 0
				if len(subSlice) >= 8 { // Check if milliseconds are available
					unixMs = binary.LittleEndian.Uint32(subSlice[4:8])
				}
				unixTotalSeconds := float64(unixSec) + float64(unixMs)/1000.0
				valStr = strconv.FormatFloat(unixTotalSeconds, 'f', 6, 64) // Format with 6 decimal places
				lastClock = valStr                                         // Update last clock time
			} else {
				log.Printf("convert2MQTT Warning: 'unixtime' field '%s' needs at least 4 bytes, got %d. Skipping.", field.Key, len(subSlice))
				continue // Skip this field
			}

		case "byte": // Treat as raw bytes potentially representing a string
			valStr = strconv.Quote(string(subSlice)) // Quote as JSON string
		case "string": // Explicitly treat as string
			valStr = strconv.Quote(string(subSlice))
		case "int8_t":
			if len(subSlice) >= 1 {
				data := int8(subSlice[0])
				val := field.Factor * float64(data)
				valStr = strconv.FormatFloat(val, 'f', -1, 64) // Use precision from factor if needed
			}
		case "uint8_t":
			if len(subSlice) >= 1 {
				data := subSlice[0]
				val := field.Factor * float64(data)
				valStr = strconv.FormatFloat(val, 'f', -1, 64)
			}
		case "int16_t":
			if len(subSlice) >= 2 {
				data := int16(binary.LittleEndian.Uint16(subSlice))
				val := field.Factor * float64(data)
				valStr = strconv.FormatFloat(val, 'f', -1, 64)
			}
		case "uint16_t":
			if len(subSlice) >= 2 {
				data := binary.LittleEndian.Uint16(subSlice)
				val := field.Factor * float64(data)
				valStr = strconv.FormatFloat(val, 'f', -1, 64)
			}
		case "int32_t":
			if len(subSlice) >= 4 {
				data := int32(binary.LittleEndian.Uint32(subSlice))
				val := field.Factor * float64(data)
				valStr = strconv.FormatFloat(val, 'f', -1, 64)
			}
		case "uint32_t":
			if len(subSlice) >= 4 {
				data := binary.LittleEndian.Uint32(subSlice)
				val := field.Factor * float64(data)
				valStr = strconv.FormatFloat(val, 'f', -1, 64)
			}
		case "float": // Assume float32
			if len(subSlice) >= 4 {
				data := math.Float32frombits(binary.LittleEndian.Uint32(subSlice))
				val := field.Factor * float64(data) // Apply factor
				valStr = strconv.FormatFloat(val, 'f', -1, 64)
			}
		default:
			log.Printf("convert2MQTT Warning: Unknown field type '%s' for key '%s'. Skipping.", field.Type, field.Key)
			continue // Skip unknown type
		}

		// Add comma if not the first field
		if fieldCount > 0 {
			retstr.WriteString(", ")
		}
		// Add "key": value pair
		retstr.WriteString(strconv.Quote(field.Key)) // Quote the key
		retstr.WriteString(": ")
		retstr.WriteString(valStr) // Value should be correctly formatted number or quoted string
		fieldCount++
	} // End field loop

	// Add last known clock time if it's not the clock message itself and fields were added
	if res.Topic != "clock" && fieldCount > 0 {
		retstr.WriteString(`, "unixtime": `)
		retstr.WriteString(lastClock)
	}

	retstr.WriteString("}")
	res.Payload = retstr.String()

	if debugMode {
		log.Printf("convert2MQTT: ID=%s -> Topic=%s, Payload=%s", idStr, res.Topic, res.Payload)
	}
	return res
}

// convert2CAN converts an MQTT topic and JSON payload string to a CAN frame.
func convert2CAN(topic, payload string) can.Frame {
	defaultFrame := can.Frame{ID: 0xFFFFFFFF, Length: 0} // Indicate error with invalid ID?

	_, convRule := getPayloadConv(topic, "mqtt2can")
	if convRule == nil {
		if debugMode {
			log.Printf("convert2CAN: No rule found for MQTT topic %s", topic)
		}
		return defaultFrame
	}

	// Parse the JSON payload into a map
	var data map[string]interface{}
	err := json.Unmarshal([]byte(payload), &data)
	if err != nil {
		log.Printf("convert2CAN Error: Failed to unmarshal JSON payload for topic %s: %v", topic, err)
		return defaultFrame
	}

	var buffer [8]uint8 // CAN frame data buffer

	// Iterate through the fields defined in the conversion rule
	for _, field := range convRule.Payload {
		value, keyExists := data[field.Key]
		if !keyExists {
			if debugMode {
				log.Printf("convert2CAN Info: Key '%s' not found in JSON payload for topic %s. Skipping field.", field.Key, topic)
			}
			continue // Skip if key is missing in JSON
		}

		// Validate place indices
		startIndex := field.Place[0]
		endIndex := field.Place[1]
		if endIndex <= startIndex {
			endIndex = startIndex + 1
		}
		if startIndex < 0 || startIndex >= 8 || endIndex <= 0 || endIndex > 8 {
			log.Printf("convert2CAN Warning: Invalid place %v for field '%s' in topic %s. Skipping field.", field.Place, field.Key, topic)
			continue
		}

		// --- Convert JSON value based on field type and place in buffer ---
		var err error = nil // Error variable for conversions
		switch field.Type {
		case "byte", "string": // Treat both as placing string bytes
			strVal, ok := value.(string)
			if !ok {
				err = fmt.Errorf("expected string for key '%s', got %T", field.Key, value)
				break
			}
			byteVal := []byte(strVal)
			count := copy(buffer[startIndex:endIndex], byteVal) // Copy bytes, respect bounds
			if count < len(byteVal) && debugMode {
				log.Printf("convert2CAN Warning: String value for key '%s' truncated.", field.Key)
			}

		case "unixtime": // Typically not sent MQTT->CAN, but ignore if present
			continue

		// Handle numeric types (assuming JSON numbers are float64)
		default:
			numVal, ok := value.(float64)
			if !ok {
				err = fmt.Errorf("expected number (float64) for key '%s', got %T", field.Key, value)
				break
			}
			// Apply inverse factor before converting to integer/float type
			valToConvert := numVal / field.Factor // Apply inverse factor

			switch field.Type {
			case "int8_t":
				if endIndex > startIndex+1 {
					endIndex = startIndex + 1
				} // Ensure single byte
				i8 := int8(valToConvert)
				buffer[startIndex] = byte(i8)
			case "uint8_t":
				if endIndex > startIndex+1 {
					endIndex = startIndex + 1
				} // Ensure single byte
				u8 := uint8(valToConvert)
				buffer[startIndex] = u8
			case "int16_t":
				if endIndex > startIndex+2 {
					endIndex = startIndex + 2
				} // Ensure 2 bytes max
				i16 := int16(valToConvert)
				binary.LittleEndian.PutUint16(buffer[startIndex:endIndex], uint16(i16))
			case "uint16_t":
				if endIndex > startIndex+2 {
					endIndex = startIndex + 2
				} // Ensure 2 bytes max
				u16 := uint16(valToConvert)
				binary.LittleEndian.PutUint16(buffer[startIndex:endIndex], u16)
			case "int32_t":
				if endIndex > startIndex+4 {
					endIndex = startIndex + 4
				} // Ensure 4 bytes max
				i32 := int32(valToConvert)
				binary.LittleEndian.PutUint32(buffer[startIndex:endIndex], uint32(i32))
			case "uint32_t":
				if endIndex > startIndex+4 {
					endIndex = startIndex + 4
				} // Ensure 4 bytes max
				u32 := uint32(valToConvert)
				binary.LittleEndian.PutUint32(buffer[startIndex:endIndex], u32)
			case "float": // Assume float32
				if endIndex > startIndex+4 {
					endIndex = startIndex + 4
				} // Ensure 4 bytes max
				f32 := float32(valToConvert)
				binary.LittleEndian.PutUint32(buffer[startIndex:endIndex], math.Float32bits(f32))
			default:
				err = fmt.Errorf("unknown field type '%s' for key '%s'", field.Type, field.Key)
			}
		} // End numeric type switch

		if err != nil {
			log.Printf("convert2CAN Warning: Conversion error for key '%s' in topic %s: %v. Skipping field.", field.Key, topic, err)
		}

	} // End field loop

	// Construct the CAN frame
	canidNr, err := strconv.ParseUint(strings.TrimPrefix(convRule.CanID, "0x"), 16, 32)
	if err != nil {
		log.Printf("convert2CAN Error: Failed to parse CAN ID '%s' for topic %s: %v", convRule.CanID, topic, err)
		return defaultFrame
	}

	frame := can.Frame{
		ID:     uint32(canidNr),
		Length: uint8(convRule.Length), // Use length specified in config (max 8 for standard CAN)
		Data:   buffer,
		// Flags, Res0, Res1 are usually 0 unless specifically needed
	}
	// Ensure Length doesn't exceed buffer size
	if frame.Length > 8 {
		frame.Length = 8
	}

	if debugMode {
		log.Printf("convert2CAN: Topic=%s -> CAN Frame: ID=%X Len=%d Data=%X", topic, frame.ID, frame.Length, frame.Data[:frame.Length])
	}
	return frame
}

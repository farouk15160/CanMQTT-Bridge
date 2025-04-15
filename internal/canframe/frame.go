package canframe

import "fmt"

// This package defines a generic CAN frame struct if needed,
// but the application currently uses `github.com/brutella/can.Frame`.
// Keep this file if you plan to use a custom frame struct later,
// otherwise, it might be removable if `brutella/can` is always used.

// Frame represents a standard CAN frame.
type Frame struct {
	// ID represents the CAN identifier (up to 29 bits).
	// Flags like EFF/RTR/ERR might be encoded within the ID depending on usage,
	// or handled separately. The brutella/can library handles this internally.
	ID     uint32
	Length uint8 // Data Length Code (0-8)
	Data   [8]uint8
	// Flags  uint8 // Separate flags if needed
	// Res0   uint8 // Reserved bytes if using specific protocols
	// Res1   uint8
}

// --- Example Helper Methods (if using this struct) ---

// IsExtended checks if the EFF flag would be set for this ID.
func (f *Frame) IsExtended() bool {
	// Standard CAN uses 11-bit ID (0x000 - 0x7FF)
	// Extended CAN uses 29-bit ID
	// A common convention is that IDs > 0x7FF are extended.
	return f.ID > 0x7FF
}

// String provides a basic string representation.
func (f *Frame) String() string {
	return fmt.Sprintf("ID: %X Len: %d Data: %X", f.ID, f.Length, f.Data[:f.Length])
}

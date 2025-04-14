package canframe

// Frame represents a CAN frame.
type Frame struct {
	// bit 0-28: CAN identifier (11/29 bit)
	// bit 29: error message flag (ERR)
	// bit 30: remote transmission request (RTR)
	// bit 31: extended frame format (EFF)
	ID     uint32
	Length uint8
	Flags  uint8
	Res0   uint8
	Res1   uint8
	Data   [8]uint8 // For CAN FD, you might need more.
}

// Add any helper methods for working with Frame objects here.
// For example, you could create a method to parse bytes or convert to JSON, etc.

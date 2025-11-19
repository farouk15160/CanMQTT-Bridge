package canframe

import "fmt"
type Frame struct {

	ID     uint32
	Length uint8 
	Data   [8]uint8
}


func (f *Frame) IsExtended() bool {
	return f.ID > 0x7FF
}

func (f *Frame) String() string {
	return fmt.Sprintf("ID: %X Len: %d Data: %X", f.ID, f.Length, f.Data[:f.Length])
}

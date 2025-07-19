package bridge

import (
	"encoding/binary"
	"log"
	"sync"
	"time"

	"github.com/brutella/can"
)

func runClockSender(wg *sync.WaitGroup) {
	defer wg.Done()
	log.Println("Clock Sender: Started.")

	// Initial ticker setup
	currentTakt := GetClockTakt()
	currentInterval := calculateInterval(currentTakt)
	ticker := time.NewTicker(currentInterval)
	targetDelayMs := 20 
	defer ticker.Stop() // Ensure ticker resources are released

	log.Printf("Clock Sender: Initial interval set to %v (%d Hz)", currentInterval, currentTakt)

	for {
		select {
		case <-clockStopChan:
			log.Println("Clock Sender: Stop signal received. Exiting.")
			return // Exit the loop and goroutine

		case <-ticker.C:
			// Check if takt has changed since last tick
			newTakt := GetClockTakt()
			newInterval := calculateInterval(newTakt)

			// Reset ticker if interval needs updating
			// Reading takt again introduces slight potential race if it changes *just* before reset,
			// but is generally safe enough. Resetting every time is less efficient.
			if newInterval != currentInterval {
				ticker.Reset(newInterval)
				currentInterval = newInterval
				log.Printf("Clock Sender: Interval updated to %v (%d Hz)", currentInterval, newTakt)
			}

			// Prepare and send CAN frame
			nowNano := time.Now().UnixNano()
			var data [8]byte // Use 8 bytes for uint64 nanoseconds
			binary.LittleEndian.PutUint64(data[:], uint64(nowNano))

			frame := can.Frame{
				ID:     0x5,  // As requested
				Length: 8,    // Fixed length for uint64 timestamp
				Data:   data, // Use the full 8-byte array
			}

			// Publish the frame (canPublish handles bus!=nil check and errors)
			err := canPublish(frame) // Uses the function from canbushandling.go

			// Optional: Log success only if debugging
			if err == nil && IsDebugEnabled() { // Use IsDebugEnabled() for thread-safe check
				log.Printf("Clock Sender: Sent time %d (ID: %X, Len: %d)", nowNano, frame.ID, frame.Length)
			}
			// Error is already logged by canPublish if it occurs
			// Schedule delayed frame
			time.AfterFunc(time.Duration(targetDelayMs)*time.Millisecond, func() {
				nowNanoDelayed := time.Now().UnixNano()
				var data [8]byte
				binary.LittleEndian.PutUint64(data[:], uint64(nowNanoDelayed))

				frame := can.Frame{
					ID:     0x6,
					Length: 8,
					Data:   data,
				}

				err := canPublish(frame)
				if err == nil && IsDebugEnabled() {
					log.Printf("Clock drive Sender: Sent time %d (ID: %X, Len: %d)", nowNanoDelayed, frame.ID, frame.Length)
				}
			})

		} // end select
	} // end for
}

func SetClockTakt(newTakt uint8) {
	ConfigLock.Lock()
	defer ConfigLock.Unlock()

	if newTakt <= 0 {
		log.Printf("Bridge Warning: Received invalid clock takt %d Hz. Must be > 0. Using previous value %d Hz.", newTakt, clockTakt)
		return // Keep the current value
	}

	if newTakt != clockTakt {
		log.Printf("Bridge Setting: Clock Takt set to: %d Hz", newTakt)
		clockTakt = newTakt
		// The running ticker in runClockSender will detect and adjust itself
	}
}
func calculateInterval(takt uint8) time.Duration {
	if takt <= 0 {
		log.Printf("Warning: Invalid takt (%d) for interval calculation, defaulting to 1 second.", takt)
		return time.Second // Default to 1 second if takt is invalid
	}
	// Calculate interval, protect against division by zero implicitly by check above
	return time.Second / time.Duration(takt)
}

// GetClockTakt returns the current clock frequency in Hz.
func GetClockTakt() uint8 {
	ConfigLock.RLock()
	defer ConfigLock.RUnlock()
	return clockTakt
}

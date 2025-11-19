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
	currentTakt := GetClockTakt()
	currentInterval := calculateInterval(currentTakt)
	ticker := time.NewTicker(currentInterval)
	defer ticker.Stop() 
	log.Printf("Clock Sender: Initial interval set to %v (%d Hz)", currentInterval, currentTakt)

	for {
		select {
		case <-clockStopChan:
			log.Println("Clock Sender: Stop signal received. Exiting.")
			return 

		case <-ticker.C:
			newTakt := GetClockTakt()
			newInterval := calculateInterval(newTakt)

			if newInterval != currentInterval {
				ticker.Reset(newInterval)
				currentInterval = newInterval
				log.Printf("Clock Sender: Interval updated to %v (%d Hz)", currentInterval, newTakt)
			}

			now := time.Now()
			nowNano := now.UnixNano() // Capture nanoseconds once

			ConfigLock.Lock()
			lastClock = nowNano // Assign nanoseconds directly (lastClock is now int64)
			ConfigLock.Unlock()

			var data [8]byte 
			binary.LittleEndian.PutUint64(data[:], uint64(nowNano))

			frame := can.Frame{
				ID:     0x5,  
				Length: 8,    
				Data:   data, 
			}

			err := canPublish(frame) 
			if err == nil && IsDebugEnabled() { 
				log.Printf("Clock Sender: Sent time %d (ID: %X, Len: %d)", nowNano, frame.ID, frame.Length)
			}
		} 
	} 
}

func SetClockTakt(newTakt uint8) {
	ConfigLock.Lock()
	defer ConfigLock.Unlock()

	if newTakt < 0 {
		log.Printf("Bridge Warning: Received invalid clock takt %d Hz. Must be > 0. Using previous value %d Hz.", newTakt, clockTakt)
		return 
	}

	if newTakt != clockTakt {
		log.Printf("Bridge Setting: Clock Takt set to: %d Hz", newTakt)
		clockTakt = newTakt
	}
}
func calculateInterval(takt uint8) time.Duration {
	if takt <= 0 {
		log.Printf("Warning: Invalid takt (%d) for interval calculation, defaulting to 1 second.", takt)
		return time.Second 
	}
	return time.Second / time.Duration(takt)
}
func GetClockTakt() uint8 {
	ConfigLock.RLock()
	defer ConfigLock.RUnlock()
	return clockTakt
}
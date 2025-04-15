package bridge

import (
	"log"
	"sync"
	"time"

	"github.com/brutella/can" // Use the external CAN library
)

// Note: Variables like 'bus', 'csi', 'csiLock' are defined in main.go
// and accessed directly by functions in this file as package variables.

var csiLock sync.Mutex // CAN subscribed IDs Mutex - Make sure only one instance

// startCanHandling initializes the CANBus Interface.
// It runs in a goroutine if configured via runInThread.
func startCanHandling(canInterface string) {
	startFunc := func() {
		if runInThread { // Only call Done if running in goroutine managed by wg
			defer wg.Done()
		}
		log.Printf("CAN Handler: Initializing CAN-Bus interface %s...", canInterface)
		var err error
		bus, err = can.NewBusForInterfaceWithName(canInterface)
		if err != nil {
			log.Fatalf("CAN Handler: Fatal error activating CAN-Bus interface %s: %v", canInterface, err)
		}
		// Subscribe to all frames and filter in handleCANFrame
		bus.SubscribeFunc(handleCANFrame)
		log.Printf("CAN Handler: Connecting and Publishing on %s...", canInterface)
		err = bus.ConnectAndPublish() // This blocks until the bus disconnects or an error occurs
		if err != nil {
			// Log fatal error if the connection/publish loop fails critically
			log.Fatalf("CAN Handler: Fatal error connecting/publishing on CAN-Bus interface %s: %v", canInterface, err)
		}
		log.Printf("CAN Handler: Disconnected from CAN interface %s.", canInterface)
	}

	if runInThread {
		log.Println("CAN Handler: Starting in separate goroutine.")
		go startFunc()
	} else {
		log.Println("CAN Handler: Starting in main thread (will block).")
		startFunc() // This will block if ConnectAndPublish blocks
	}
}

// handleCANFrame is called by the CAN library for every received frame.
func handleCANFrame(frame can.Frame) {
	// Correctly mask the ID to get the 29-bit identifier (removes flags)
	// Use the standard 0x1FFFFFFF mask.
	idToMatch := frame.ID & 0x1FFFFFFF

	idSubscribed := false
	csiLock.Lock() // Protect access to csi slice
	for _, subscribedID := range csi {
		if subscribedID == idToMatch {
			idSubscribed = true
			break
		}
	}
	csiLock.Unlock()

	if idSubscribed {
		if debugMode {
			log.Printf("CAN Handler: ID %X is subscribed. Processing frame.", idToMatch)
		}
		if timeSleepValue > 0 {
			time.Sleep(timeSleepValue)
		}
		handleCAN(frame) // Call the processing function in receivehandling.go
	} else {
		if debugMode {
			// Reduce log spam for unsubscribed IDs by commenting out or using less frequent logging
			// log.Printf("CAN Handler: ID %X not subscribed. Frame ignored.", idToMatch)
		}
	}
}

// canSubscribe adds a CAN ID to the subscription list.
func canSubscribe(id uint32) {
	csiLock.Lock()
	// Avoid duplicates
	found := false
	for _, existingID := range csi {
		if existingID == id {
			found = true
			break
		}
	}
	if !found {
		csi = append(csi, id)
		if debugMode {
			log.Printf("CAN Handler: Subscribed to CAN ID: %X", id)
		}
	} else {
		if debugMode {
			log.Printf("CAN Handler: CAN ID %X already subscribed.", id)
		}
	}
	csiLock.Unlock()
}

// clearCanSubscriptions removes all CAN ID subscriptions.
func clearCanSubscriptions() {
	csiLock.Lock()
	if len(csi) > 0 {
		log.Println("CAN Handler: Clearing existing CAN subscriptions.")
		csi = []uint32{} // Reset the slice
	}
	csiLock.Unlock()
}

// canPublish sends a CAN frame to the bus.
func canPublish(frame can.Frame) {
	if bus == nil {
		log.Println("Error: CAN bus not initialized. Cannot publish frame.")
		return
	}
	if debugMode {
		log.Printf("CAN Handler: Publishing CAN Frame: ID=%X Len=%d Data=%X", frame.ID, frame.Length, frame.Data[:frame.Length])
	}

	// The brutella/can library should handle setting EFF flag correctly based on ID > 0x7FF.
	// No need to manually set frame.ID |= 0x80000000 usually.

	err := bus.Publish(frame)
	if err != nil {
		// Log non-fatal error, allow application to continue if possible
		log.Printf("CAN Handler: Error publishing CAN frame (ID: %X): %v", frame.ID, err)
		// Consider adding retry logic or specific error handling here
	}
}

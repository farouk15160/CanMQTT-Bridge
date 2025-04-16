package bridge

import (
	"fmt" // Added for error formatting
	"log"
	"sync"

	"github.com/brutella/can" // Use the external CAN library
)

// Note: Variables like 'bus', 'csi', 'csiLock', 'timeSleepValue', 'debugMode'
// are defined in main.go and accessed directly as package variables.

var csiLock sync.Mutex // CAN subscribed IDs Mutex

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
			// No recovery possible here, so Fatal is appropriate
		}
		// Subscribe to all frames initially, filtering happens in handleCANFrame
		bus.SubscribeFunc(handleCANFrame)

		log.Printf("CAN Handler: Connecting and starting publish loop on %s...", canInterface)
		err = bus.ConnectAndPublish() // This blocks until the bus disconnects or an error occurs
		if err != nil {
			// Log fatal error if the connection/publish loop fails critically
			// This indicates the CAN interface is likely down.
			log.Fatalf("CAN Handler: Fatal error in CAN bus connection/publish loop on %s: %v", canInterface, err)
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
// It filters based on subscribed IDs and calls handleCAN for processing.
func handleCANFrame(frame can.Frame) {
	// Correctly mask the ID to get the 29-bit identifier (removes flags)
	// Use the standard 0x1FFFFFFF mask for 29-bit IDs.
	// For standard 11-bit IDs, the mask would be 0x7FF.
	// Assuming extended IDs are possible/used.
	idToMatch := frame.ID & 0x1FFFFFFF

	idSubscribed := false
	csiLock.Lock() // Protect access to csi slice
	// Check if the ID is in our subscription list
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
		// Call the processing function in receivehandling.go
		// handleCAN now includes the time.Sleep logic internally
		handleCAN(frame)
	} else {
		// Reduce log spam for unsubscribed IDs, only log if debugMode is explicitly high?
		// if debugMode {
		//	 log.Printf("CAN Handler: ID %X not subscribed. Frame ignored.", idToMatch)
		// }
	}
}

// canSubscribe adds a CAN ID to the subscription list if not already present.
func canSubscribe(id uint32) {
	csiLock.Lock()
	defer csiLock.Unlock() // Ensure unlock even if errors occur later (though unlikely here)

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
			// log.Printf("CAN Handler: CAN ID %X already subscribed.", id) // Reduce noise
		}
	}
}

// clearCanSubscriptions removes all CAN ID subscriptions.
func clearCanSubscriptions() {
	csiLock.Lock()
	defer csiLock.Unlock()
	if len(csi) > 0 {
		log.Println("CAN Handler: Clearing existing CAN subscriptions.")
		csi = []uint32{} // Reset the slice
	}
}

// canPublish sends a CAN frame to the bus.
// It now returns an error if the publish operation fails.
func canPublish(frame can.Frame) error {
	if bus == nil {
		err := fmt.Errorf("CAN bus not initialized, cannot publish frame ID %X", frame.ID)
		log.Printf("Error: %v", err)
		return err
	}

	if debugMode {
		log.Printf("CAN Handler: Publishing CAN Frame: ID=%X Len=%d Data=%X", frame.ID, frame.Length, frame.Data[:frame.Length])
	}

	// The brutella/can library handles setting EFF/RTR flags based on ID and frame properties.
	err := bus.Publish(frame)
	if err != nil {
		// Log non-fatal error, allow application to continue if possible
		log.Printf("CAN Handler: Error publishing CAN frame (ID: %X): %v", frame.ID, err)
		// Consider adding retry logic or specific error handling here if needed
		return err // Return the error
	}
	// Success
	return nil
}

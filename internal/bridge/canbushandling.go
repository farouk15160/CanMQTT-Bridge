package bridge

import (
	"fmt"
	"log"
	"strings"
	"sync"

	// Added for potential future use (e.g., timeouts)
	"github.com/brutella/can"
)

// csiLock still protects the 'csi' slice used for filtering
var csiLock sync.Mutex

// startCanHandling initializes the CANBus Interface.
func startCanHandling(canInterface string) {
	// Ensure wg.Done is called when this function/goroutine exits
	defer func() {
		log.Println("CAN Handler: Exiting...")
		if r := recover(); r != nil {
			log.Printf("CAN Handler PANICKED: %v", r)
			// Optionally perform more cleanup or logging on panic
		}
		wg.Done() // Decrement waitgroup counter for the CAN handler task
	}()

	log.Printf("CAN Handler: Initializing CAN-Bus interface %s...", canInterface)
	var err error
	bus, err = can.NewBusForInterfaceWithName(canInterface)
	if err != nil {
		// Use Fatalf only if it should terminate the whole app. Otherwise, log and maybe exit goroutine?
		log.Printf("CAN Handler: Fatal error activating CAN-Bus interface %s: %v. Handler exiting.", canInterface, err)
		// Depending on requirements, maybe trigger app shutdown here?
		return // Exit this function/goroutine
	}

	// Subscribe using the filtering dispatcher
	bus.SubscribeFunc(dispatchCANFrame)

	log.Printf("CAN Handler: Connecting and starting publish loop on %s...", canInterface)
	// Run ConnectAndPublish in a separate goroutine so we can select on stopChan
	connectAndPublishDone := make(chan struct{}) // Signal channel for completion
	go func() {
		defer close(connectAndPublishDone) // Signal completion when done
		err := bus.ConnectAndPublish()     // This blocks
		if err != nil {
			// Log error if the loop terminates unexpectedly
			// Avoid logging "interrupted" error if caused by bus.Disconnect() during shutdown
			if !strings.Contains(err.Error(), "interrupted") { // Adjust string check as needed for your CAN library
				log.Printf("CAN Handler: Error in CAN bus connection/publish loop on %s: %v", canInterface, err)
			}
		}
		log.Printf("CAN Handler: ConnectAndPublish loop terminated for %s.", canInterface)
		// If ConnectAndPublish exits, we might want to signal the main app or attempt reconnection?
		// For now, it just stops processing CAN frames.
	}()

	log.Printf("CAN Handler: Running. Waiting for stop signal or disconnect...")

	// Wait for EITHER the stop signal OR the ConnectAndPublish loop to finish
	select {
	case <-stopChan: // Wait until stopChan is closed
		log.Println("CAN Handler: Stop signal received.")
		// Disconnect is handled in Stop() function now
	case <-connectAndPublishDone:
		log.Println("CAN Handler: ConnectAndPublish loop finished unexpectedly.")
		// Optional: Signal main application or attempt recovery
	}
}

// dispatchCANFrame filters incoming frames and sends them to the worker channel.
func dispatchCANFrame(frame can.Frame) {
	// Mask ID correctly
	idToMatch := frame.ID & 0x1FFFFFFF

	// Check if subscribed using the local 'csi' slice
	idSubscribed := false
	csiLock.Lock()
	// Optimization: If 'csi' becomes very large, consider a map[uint32]struct{} for faster lookups
	for _, subscribedID := range csi {
		if subscribedID == idToMatch {
			idSubscribed = true
			break
		}
	}
	csiLock.Unlock()

	if idSubscribed {
		// Send to worker channel instead of processing directly
		select {
		case <-stopChan: // Check stop channel first to prevent sending to closed channel
			// log.Printf("CAN Dispatcher: Stop signal received while dispatching ID %X. Exiting dispatch.", idToMatch) // Reduce noise
			return // Stop dispatching if stop signal received
		case canWorkChan <- frame:
			// Successfully dispatched
			// Logging here is too verbose for high throughput
			// if debugMode { log.Printf("CAN Dispatcher: Frame ID %X sent to worker.", idToMatch) }
		default:
			// Channel full - log if debugging, indicates workers can't keep up
			ConfigLock.RLock() // Safely read debug mode
			dbg := debugMode
			ConfigLock.RUnlock()
			if dbg {
				log.Printf("CAN Dispatcher Warning: CAN work channel full. Discarding frame ID %X.", idToMatch)
			}
			// Optional: Increment dropped message counter (using atomic operations for safety)
		}
	}
	// Ignore unsubscribed frames silently unless debugging is very high
}

// canSubscribe is NO LONGER USED directly. subscribeInitialCanIDs updates 'csi'.

// clearCanSubscriptions removes all CAN ID subscriptions from the local 'csi' list.
func clearCanSubscriptions() {
	csiLock.Lock()
	defer csiLock.Unlock()
	if len(csi) > 0 {
		log.Println("CAN Handler: Clearing existing CAN subscriptions filter list.")
		csi = []uint32{} // Reset the slice
	}
}

// canPublish sends a CAN frame. Logs errors.
func canPublish(frame can.Frame) error {
	// Bus check might be redundant if startCanHandling ensures bus is valid
	if bus == nil {
		err := fmt.Errorf("CAN bus not initialized, cannot publish frame ID %X", frame.ID)
		log.Printf("Error: %v", err)
		return err
	}

	// Debug log moved to caller (mqttProcessor)

	err := bus.Publish(frame)
	if err != nil {
		log.Printf("CAN Handler: Error publishing CAN frame (ID: %X): %v", frame.ID, err)
		// Return error for caller to handle if needed
		return err
	}
	return nil // Success
}

package bridge

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"github.com/brutella/can"
)

var csiLock sync.Mutex
func startCanHandling(canInterface string) {
	defer func() {
		log.Println("CAN Handler: Exiting...")
		if r := recover(); r != nil {
			log.Printf("CAN Handler PANICKED: %v", r)
		}
		wg.Done() 
	}()

	log.Printf("CAN Handler: Initializing CAN-Bus interface %s...", canInterface)
	var err error
	bus, err = can.NewBusForInterfaceWithName(canInterface)
	if err != nil {
		log.Printf("CAN Handler: Fatal error activating CAN-Bus interface %s: %v. Handler exiting.", canInterface, err)
		return 
	}

	bus.SubscribeFunc(dispatchCANFrame)

	log.Printf("CAN Handler: Connecting and starting publish loop on %s...", canInterface)
	connectAndPublishDone := make(chan struct{}) // Signal channel for completion
	go func() {
		defer close(connectAndPublishDone) // Signal completion when done
		err := bus.ConnectAndPublish()     // This blocks
		if err != nil {
			if !strings.Contains(err.Error(), "interrupted") { // Adjust string check as needed for your CAN library
				log.Printf("CAN Handler: Error in CAN bus connection/publish loop on %s: %v", canInterface, err)
			}
		}
		log.Printf("CAN Handler: ConnectAndPublish loop terminated for %s.", canInterface)
	}()

	log.Printf("CAN Handler: Running. Waiting for stop signal or disconnect...")
	select {
	case <-stopChan: 
		log.Println("CAN Handler: Stop signal received.")
	case <-connectAndPublishDone:
		log.Println("CAN Handler: ConnectAndPublish loop finished unexpectedly.")

	}
}

func dispatchCANFrame(frame can.Frame) {
	// Mask ID correctly
	idToMatch := frame.ID & 0x1FFFFFFF

	idSubscribed := false
	csiLock.Lock()
	for _, subscribedID := range csi {
		if subscribedID == idToMatch {
			idSubscribed = true
			break
		}
	}
	csiLock.Unlock()

	if idSubscribed {
		select {
		case <-stopChan: // Check stop channel first to prevent sending to closed channel
			return // Stop dispatching if stop signal received
		case canWorkChan <- frame:
			if debugMode { log.Printf("CAN Dispatcher: Frame ID %X sent to worker.", idToMatch) }
		default:
			ConfigLock.RLock() // Safely read debug mode
			dbg := debugMode
			ConfigLock.RUnlock()
			if dbg {
				log.Printf("CAN Dispatcher Warning: CAN work channel full. Discarding frame ID %X.", idToMatch)
			}
		}
	}
}

func clearCanSubscriptions() {
	csiLock.Lock()
	defer csiLock.Unlock()
	if len(csi) > 0 {
		log.Println("CAN Handler: Clearing existing CAN subscriptions filter list.")
		csi = []uint32{} // Reset 
	}
}

func canPublish(frame can.Frame) error {
	if bus == nil {
		err := fmt.Errorf("CAN bus not initialized, cannot publish frame ID %X", frame.ID)
		log.Printf("Error: %v", err)
		return err
	}

	err := bus.Publish(frame)
	if err != nil {
		log.Printf("CAN Handler: Error publishing CAN frame (ID: %X): %v", frame.ID, err)
		return err
	}
	return nil // Success
}

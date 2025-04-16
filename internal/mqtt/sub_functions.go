package mqtt

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"strconv"
	"strings"
	"time"
)

// getBufferUsage provides a placeholder for buffer monitoring.
// *** YOU MUST ADAPT THIS FUNCTION ***
// Replace this with logic that measures your actual application's buffer(s).
// Examples:
// - If using a channel `myChan chan T`: return len(myChan) // Number of items currently in buffer
// - If using a channel `myChan chan T`: return int(float64(len(myChan)) / float64(cap(myChan)) * 100) // Percentage full
// - If using a slice `mySlice []T`: return len(mySlice)
func getBufferUsage() int {
	// Placeholder implementation: returns 0
	// TODO: Implement actual buffer usage measurement here.
	log.Println("Warning: getBufferUsage() is using a placeholder implementation.")
	return 0 // Replace with your actual buffer measurement
}

func getTotalMemory() uint64 {
	// Placeholder: Return a default value or implement the OS-specific logic.
	// This could be improved by reading /proc/meminfo on Linux
	return 16 * 1024 * 1024 * 1024 // 16GB (default value) - Consider making this more dynamic
}

// getCPUUsage calculates overall CPU usage based on /proc/stat (Linux specific).
// It returns a slice containing one float64 representing the overall usage (0.0 to 1.0).

var prevCPUTimes []uint64 // Stores previous CPU times for calculation

func getCPUUsage() []float64 {
	contents, err := ioutil.ReadFile("/proc/stat")
	if err != nil {
		log.Printf("Error reading /proc/stat: %v", err)
		return []float64{0.0} // Error case
	}
	lines := strings.Split(string(contents), "\n")

	var currentTimes []uint64 // To store times from the overall "cpu " line

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "cpu" { // Find the overall "cpu" line (not "cpu0", "cpu1", etc.)
			currentTimes = make([]uint64, 0, len(fields)-1)
			for i := 1; i < len(fields); i++ {
				value, err := strconv.ParseUint(fields[i], 10, 64)
				if err != nil {
					log.Printf("Error parsing CPU time field '%s' in line '%s': %v", fields[i], line, err)
					return []float64{0.0} // Error on parsing
				}
				currentTimes = append(currentTimes, value)
			}
			break // Found the overall "cpu" line
		}
	}

	if len(currentTimes) < 4 { // Need at least user, nice, system, idle
		log.Println("Could not find sufficient CPU data (user, nice, system, idle) in /proc/stat")
		return []float64{0.0} // Error case
	}

	// Calculate total time and idle time from current values
	var currentTotalTime, currentIdleTime uint64
	currentIdleTime = currentTimes[3] // idle is usually the 4th field (index 3)
	for _, t := range currentTimes {
		currentTotalTime += t
	}

	// Calculate usage if previous times are available
	var cpuUsage float64 = 0.0
	if prevCPUTimes != nil && len(prevCPUTimes) >= 4 {
		var prevTotalTime, prevIdleTime uint64
		prevIdleTime = prevCPUTimes[3]
		for _, t := range prevCPUTimes {
			prevTotalTime += t
		}

		deltaTotal := currentTotalTime - prevTotalTime
		deltaIdle := currentIdleTime - prevIdleTime

		if deltaTotal > 0 {
			cpuUsage = 1.0 - (float64(deltaIdle) / float64(deltaTotal))
		}
		// Prevent usage from going below 0 or above 1 due to potential timing issues/wraparounds
		if cpuUsage < 0.0 {
			cpuUsage = 0.0
		}
		if cpuUsage > 1.0 {
			cpuUsage = 1.0
		}

	} else {
		// This is the first call, or previous data was invalid.
		// We can't calculate usage yet. We'll store current times and return 0.
		// The next call will have previous data.
		log.Println("Initializing CPU usage calculation. First reading might be 0%.")
	}

	// Store current times for the next calculation
	prevCPUTimes = make([]uint64, len(currentTimes))
	copy(prevCPUTimes, currentTimes)

	// Return overall CPU usage in a slice for consistency
	return []float64{cpuUsage}
}

// getTemperature reads system temperature (Linux specific examples).
func getTemperature() float32 {
	// Common paths for thermal zones on Linux
	tempPaths := []string{
		"/sys/class/thermal/thermal_zone0/temp",
		"/sys/class/thermal/thermal_zone1/temp",
		// Add more paths if needed, e.g., for different hardware
	}

	for _, path := range tempPaths {
		contents, err := ioutil.ReadFile(path)
		if err == nil { // Successfully read a file
			tempStr := strings.TrimSpace(string(contents))
			tempInt, err := strconv.Atoi(tempStr)
			if err == nil {
				// Temperature is usually in milli-Celsius
				return float32(tempInt) / 1000.0
			} else {
				log.Printf("Error converting temperature string '%s' from path '%s': %v", tempStr, path, err)
			}
		}
	}

	// If no path worked
	log.Printf("Warning: Could not read temperature from common paths.")
	return -1.0 // Indicate unavailable temperature
}

// getUptime reads system uptime from /proc/uptime (Linux).
func getUptime() uint64 {
	contents, err := ioutil.ReadFile("/proc/uptime")
	if err != nil {
		log.Printf("Error reading /proc/uptime: %v", err)
		return 0 // Error case
	}
	// The first field is the uptime in seconds
	fields := strings.Fields(string(contents))
	if len(fields) == 0 {
		log.Printf("Error: /proc/uptime format unexpected: %s", string(contents))
		return 0
	}
	uptimeStr := fields[0]
	uptimeFloat, err := strconv.ParseFloat(uptimeStr, 64)
	if err != nil {
		log.Printf("Error converting uptime string '%s': %v", uptimeStr, err)
		return 0 // Error case
	}
	return uint64(uptimeFloat) // Uptime is in seconds
}

// formatUptime converts seconds into a human-readable string.
func formatUptime(seconds uint64) string {
	if seconds == 0 {
		return "N/A"
	}
	duration := time.Duration(seconds) * time.Second
	days := int(duration.Hours() / 24)
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60
	secondsOnly := int(duration.Seconds()) % 60
	return fmt.Sprintf("%d days, %d hours, %d minutes, %d seconds", days, hours, minutes, secondsOnly)
}

// getIPAddress tries to find the primary local non-loopback IPv4 address.
func getIPAddress() string {
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, address := range addrs {
			// Check the address type and if it is not a loopback
			if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil { // Check if it's an IPv4 address
					return ipnet.IP.String()
				}
			}
		}
	} else {
		log.Printf("Error getting interface addresses: %v", err)
	}
	log.Println("Warning: Could not determine local IP address.")
	return "unknown"
}

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

// getBufferUsage is a placeholder. Replace with your actual logic.
func getBufferUsage() int {
	// Example: If you have a queue, return its length.
	//  return myQueue.Length()
	// Example: If you have a buffer, return its fill percentage.
	//  return int(float64(myBuffer.CurrentSize()) / float64(myBuffer.MaxSize()) * 100)
	return 0 // Placeholder
}
func getTotalMemory() uint64 {
	// Placeholder: Return a default value or implement the OS-specific logic.
	return 16 * 1024 * 1024 * 1024 // 16GB (default value)
}
// getCPUUsage is a placeholder. Implement this accurately for your OS.

var prevCPUTimes []uint64

func getCPUUsage() []float64 {
	contents, err := ioutil.ReadFile("/proc/stat")
	if err != nil {
		log.Printf("Error reading /proc/stat: %v", err)
		return []float64{0.0} // Error case
	}
	lines := strings.Split(string(contents), "\n")

	cpuTimes := make([]uint64, 0)
	for _, line := range lines {
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			for i := 1; i < len(fields); i++ {
				value, err := strconv.ParseUint(fields[i], 10, 64)
				if err != nil {
					log.Printf("Error parsing CPU time: %v", err)
					return []float64{0.0} // Error case
				}
				cpuTimes = append(cpuTimes, value)
			}
			break
		}
	}

	if len(cpuTimes) == 0 {
		log.Println("Could not find CPU data in /proc/stat")
		return []float64{0.0} // Error case
	}

	if prevCPUTimes == nil {
		prevCPUTimes = make([]uint64, len(cpuTimes))
		copy(prevCPUTimes, cpuTimes)
		time.Sleep(100 * time.Millisecond) // Short delay for measurement
		return getCPUUsage()               // Recursive call after initialization
	}

	cpuUsages := make([]float64, len(cpuTimes))
	totalDiff := uint64(0)
	for i, currentTime := range cpuTimes {
		diff := currentTime - prevCPUTimes[i]
		totalDiff += diff
		cpuUsages[i] = float64(diff)
		prevCPUTimes[i] = currentTime
	}

	for i := range cpuUsages {
		cpuUsages[i] = cpuUsages[i] / float64(totalDiff)
	}

	return cpuUsages
}

// getTemperature is a placeholder. Implement this for your OS.
func getTemperature() float32 {
	// Raspberry Pi CPU Temperature
	contents, err := ioutil.ReadFile("/sys/class/thermal/thermal_zone0/temp")
	if err != nil {
		log.Printf("Error reading temperature: %v", err)
		return 0.0 // Error case
	}
	tempStr := strings.TrimSpace(string(contents))
	tempInt, err := strconv.Atoi(tempStr)
	if err != nil {
		log.Printf("Error converting temperature: %v", err)
		return 0.0 // Error case
	}
	return float32(tempInt) / 1000.0 // Temperature is in milli-celsius
}

// getUptime is a placeholder. Implement this for your OS.
func getUptime() uint64 {
	contents, err := ioutil.ReadFile("/proc/uptime")
	if err != nil {
		log.Printf("Error reading /proc/uptime: %v", err)
		return 0 // Error case
	}
	uptimeStr := strings.Fields(string(contents))[0]
	uptimeFloat, err := strconv.ParseFloat(uptimeStr, 64)
	if err != nil {
		log.Printf("Error converting uptime: %v", err)
		return 0 // Error case
	}
	return uint64(uptimeFloat) // Uptime is in seconds
}
func formatUptime(seconds uint64) string {
	duration := time.Duration(seconds) * time.Second
	days := int(duration.Hours() / 24)
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60
	secondsOnly := int(duration.Seconds()) % 60
	return fmt.Sprintf("%d days, %d hours, %d minutes, %d seconds", days, hours, minutes, secondsOnly)
}

// getIPAddress tries to find the primary local IPv4 address.

func getIPAddress() string {
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, address := range addrs {
			if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
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

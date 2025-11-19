package mqtt

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

func getBufferUsage() BufferUsage {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		log.Printf("Error opening /proc/meminfo: %v", err)
		return BufferUsage{}
	}
	defer file.Close()

	var buffers, cached, memTotal, memAvailable uint64
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		key := fields[0]
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}

		switch key {
		case "Buffers:":
			buffers = value
		case "Cached:":
			cached = value
		case "MemTotal:":
			memTotal = value
		case "MemAvailable:":
			memAvailable = value
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading /proc/meminfo: %v", err)
		return BufferUsage{}
	}


	usedKB := buffers + cached
	usedMB := int(usedKB / 1024)
	availableMB := int(memAvailable / 1024)

	usedPercent := 0.0
	if memTotal > 0 {
		usedPercent = (float64(usedKB) / float64(memTotal)) * 100
	}

	return BufferUsage{
		UsedMB:      usedMB,
		AvailableMB: availableMB,
		UsedPercent: usedPercent,
	}
}

func getTotalMemory() uint64 {

	return 16 * 1024 * 1024 * 1024 
}

var prevCPUTimes []uint64 

func getCPUUsage() []float64 {
	contents, err := ioutil.ReadFile("/proc/stat")
	if err != nil {
		log.Printf("Error reading /proc/stat: %v", err)
		return []float64{0.0} 
	}
	lines := strings.Split(string(contents), "\n")

	var currentTimes []uint64 

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "cpu" { // Find the overall "cpu" line (not "cpu0", "cpu1", etc.)
			currentTimes = make([]uint64, 0, len(fields)-1)
			for i := 1; i < len(fields); i++ {
				value, err := strconv.ParseUint(fields[i], 10, 64)
				if err != nil {
					log.Printf("Error parsing CPU time field '%s' in line '%s': %v", fields[i], line, err)
					return []float64{0.0}
				}
				currentTimes = append(currentTimes, value)
			}
			break 
		}
	}

	if len(currentTimes) < 4 {
		log.Println("Could not find sufficient CPU data (user, nice, system, idle) in /proc/stat")
		return []float64{0.0} 
	}

	
	var currentTotalTime, currentIdleTime uint64
	currentIdleTime = currentTimes[3] 
	for _, t := range currentTimes {
		currentTotalTime += t
	}

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
	
		if cpuUsage < 0.0 {
			cpuUsage = 0.0
		}
		if cpuUsage > 1.0 {
			cpuUsage = 1.0
		}

	} else {
	
		log.Println("Initializing CPU usage calculation. First reading might be 0%.")
	}

	prevCPUTimes = make([]uint64, len(currentTimes))
	copy(prevCPUTimes, currentTimes)

	return []float64{cpuUsage}
}

func getTemperature() float32 {
	
	tempPaths := []string{
		"/sys/class/thermal/thermal_zone0/temp",
		"/sys/class/thermal/thermal_zone1/temp",
		
	}

	for _, path := range tempPaths {
		contents, err := ioutil.ReadFile(path)
		if err == nil {
			tempStr := strings.TrimSpace(string(contents))
			tempInt, err := strconv.Atoi(tempStr)
			if err == nil {
				return float32(tempInt) / 1000.0
			} else {
				log.Printf("Error converting temperature string '%s' from path '%s': %v", tempStr, path, err)
			}
		}
	}

	log.Printf("Warning: Could not read temperature from common paths.")
	return -1.0
}

func getUptime() uint64 {
	contents, err := ioutil.ReadFile("/proc/uptime")
	if err != nil {
		log.Printf("Error reading /proc/uptime: %v", err)
		return 0 
	}
	fields := strings.Fields(string(contents))
	if len(fields) == 0 {
		log.Printf("Error: /proc/uptime format unexpected: %s", string(contents))
		return 0
	}
	uptimeStr := fields[0]
	uptimeFloat, err := strconv.ParseFloat(uptimeStr, 64)
	if err != nil {
		log.Printf("Error converting uptime string '%s': %v", uptimeStr, err)
		return 0
	}
	return uint64(uptimeFloat) 
}

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

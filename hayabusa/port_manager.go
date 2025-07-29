package hayabusa

import (
	"crypto/rand"
	"fmt"
	"net"
	"sync"
)

type PortManager struct {
	ports     map[int]bool     // port -> used
	portsByID map[string][]int // eg. "test-ID-1" -> [30001, 30002, ...]
	mu        sync.Mutex
}

var globalPortManager = &PortManager{
	ports:     make(map[int]bool),
	portsByID: make(map[string][]int),
}

func (pm *PortManager) NewPort(id string) int {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.portsByID[id]; !exists {
		pm.portsByID[id] = make([]int, 0)
	}

	port := pm.rndPort()
	pm.ports[port] = true
	pm.portsByID[id] = append(pm.portsByID[id], port)

	return port
}

func (pm *PortManager) RemovePorts(id string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	portsByID, exists := pm.portsByID[id]
	if !exists {
		return
	}

	for _, port := range portsByID {
		delete(pm.ports, port)
	}
	delete(pm.portsByID, id)
}

// rndPort generates a random port using global synchronization to avoid race conditions
func (pm *PortManager) rndPort() int {
	const (
		minPort     = 49152
		maxPort     = 65535
		maxAttempts = 100
	)

	attempts := 0
	for attempts < maxAttempts {
		buf := make([]byte, 2)
		// Ignoring the error for brevity—not recommended in production code!
		_, _ = rand.Read(buf)

		// Convert 2 bytes to a 16-bit number, then mod by the range size.
		n := int(buf[0])<<8 | int(buf[1])
		port := minPort + (n % (maxPort - minPort + 1))

		// Check if port is not in our global map AND actually available
		if !pm.ports[port] && isPortAvailable(port) {
			pm.ports[port] = true
			return port
		}
		attempts++
	}

	// If we can't find an available port after maxAttempts,
	// try sequential search starting from minPort
	for port := minPort; port <= maxPort; port++ {
		if !pm.ports[port] && isPortAvailable(port) {
			pm.ports[port] = true
			return port
		}
	}

	panic(fmt.Sprintf("no available ports found in range %d-%d", minPort, maxPort))
}

// isPortAvailable checks if a port is actually available for binding
func isPortAvailable(port int) bool {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

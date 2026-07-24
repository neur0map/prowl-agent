package workbench

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
)

// ListenLoopback opens the workbench only on IPv4 loopback. Port zero asks the
// operating system to choose an unused ephemeral port.
func ListenLoopback(port int) (net.Listener, error) {
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("invalid workbench port %d", port)
	}
	return net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
}

// NewBearerToken returns 256 bits of cryptographic entropy for an in-memory workbench credential.
func NewBearerToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate workbench bearer token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

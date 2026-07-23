package workbench

import (
	"net"
	"testing"
)

func TestListenLoopbackBindsOnlyIPv4Loopback(t *testing.T) {
	listener, err := ListenLoopback(0)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("address type=%T", listener.Addr())
	}
	if !address.IP.Equal(net.ParseIP("127.0.0.1")) {
		t.Fatalf("listener bound to %s", address.IP)
	}
	if address.Port == 0 {
		t.Fatal("ephemeral port was not assigned")
	}
}

func TestNewBearerTokenHasCryptographicEntropy(t *testing.T) {
	first, err := NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("generated duplicate bearer tokens")
	}
	if len(first) < 40 {
		t.Fatalf("token length=%d want at least 40 URL-safe characters", len(first))
	}
}

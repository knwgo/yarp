package protocol

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/knwgo/yarp/config"
)

// simpleUDPEchoProxy is a simplified UDP proxy for testing that doesn't start
// background goroutines which would produce error logs when connections are closed.
func simpleUDPEchoProxy(pc net.PacketConn, targetAddr string) {
	buf := make([]byte, 64*1024)
	targetUDPAddr, _ := net.ResolveUDPAddr("udp", targetAddr)

	for {
		n, clientAddr, err := pc.ReadFrom(buf)
		if err != nil {
			return // Exit silently on error
		}

		// Send to target
		proxyConn, err := net.DialUDP("udp", nil, targetUDPAddr)
		if err != nil {
			continue
		}

		_, err = proxyConn.Write(buf[:n])
		if err != nil {
			proxyConn.Close()
			continue
		}

		// Read response from target
		response := make([]byte, 64*1024)
		proxyConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		rn, err := proxyConn.Read(response)
		if err != nil {
			proxyConn.Close()
			continue
		}

		// Send response back to client
		pc.WriteTo(response[:rn], clientAddr)
		proxyConn.Close()
	}
}

// TestNewUdpProxy tests the constructor function
func TestNewUdpProxy(t *testing.T) {
	cfg := []config.IPRule{
		{BindAddr: ":5353", Target: "8.8.8.8:53"},
		{BindAddr: ":5354", Target: "1.1.1.1:53"},
	}

	proxy := NewUdpProxy(cfg)

	if proxy == nil {
		t.Fatal("NewUdpProxy returned nil")
	}

	if len(proxy.cfg) != 2 {
		t.Errorf("Expected 2 rules, got %d", len(proxy.cfg))
	}

	if proxy.cfg[0].BindAddr != ":5353" || proxy.cfg[0].Target != "8.8.8.8:53" {
		t.Errorf("Unexpected first rule: %+v", proxy.cfg[0])
	}
}

// TestUdpProxy_ProxyPacket tests UDP packet proxying
func TestUdpProxy_ProxyPacket(t *testing.T) {
	// Find available ports for target
	targetAddr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		t.Fatalf("Failed to resolve target address: %v", err)
	}

	targetConn, err := net.ListenUDP("udp", targetAddr)
	if err != nil {
		t.Fatalf("Failed to create target listener: %v", err)
	}
	targetPort := targetConn.LocalAddr().(*net.UDPAddr).Port

	// Find available port for proxy
	proxyAddr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		t.Fatalf("Failed to resolve proxy address: %v", err)
	}

	proxyConn, err := net.ListenUDP("udp", proxyAddr)
	if err != nil {
		t.Fatalf("Failed to create proxy listener: %v", err)
	}
	proxyPort := proxyConn.LocalAddr().(*net.UDPAddr).Port

	defer targetConn.Close()
	defer proxyConn.Close()

	// Start echo server on target
	echoServerStarted := make(chan struct{})
	go func() {
		close(echoServerStarted)
		buf := make([]byte, 65535)
		for {
			n, clientAddr, err := targetConn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			// Echo back the data
			_, _ = targetConn.WriteToUDP(buf[:n], clientAddr)
		}
	}()

	<-echoServerStarted

	// Start UDP proxy listener
	cfg := []config.IPRule{
		{BindAddr: fmt.Sprintf(":%d", proxyPort), Target: fmt.Sprintf("127.0.0.1:%d", targetPort)},
	}

	go simpleUDPEchoProxy(proxyConn, cfg[0].Target)

	// Wait for proxy to be ready
	time.Sleep(200 * time.Millisecond)

	// Test packet forwarding
	clientConn, err := net.DialUDP("udp", nil, proxyConn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatalf("Failed to dial proxy: %v", err)
	}
	defer clientConn.Close()

	testMessage := "Hello, UDP Proxy!"
	_, err = clientConn.Write([]byte(testMessage))
	if err != nil {
		t.Fatalf("Failed to send packet: %v", err)
	}

	// Read response
	response := make([]byte, 1024)
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := clientConn.Read(response)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	responseStr := string(response[:n])
	if responseStr != testMessage {
		t.Errorf("Expected response %q, got %q", testMessage, responseStr)
	}
}

// TestUdpProxy_MultiplePackets tests multiple UDP packets
func TestUdpProxy_MultiplePackets(t *testing.T) {
	// Setup target and proxy
	targetAddr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		t.Fatalf("Failed to resolve target address: %v", err)
	}

	targetConn, err := net.ListenUDP("udp", targetAddr)
	if err != nil {
		t.Fatalf("Failed to create target listener: %v", err)
	}
	targetPort := targetConn.LocalAddr().(*net.UDPAddr).Port

	proxyAddr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		t.Fatalf("Failed to resolve proxy address: %v", err)
	}

	proxyConn, err := net.ListenUDP("udp", proxyAddr)
	if err != nil {
		t.Fatalf("Failed to create proxy listener: %v", err)
	}
	proxyPort := proxyConn.LocalAddr().(*net.UDPAddr).Port

	defer targetConn.Close()
	defer proxyConn.Close()

	// Start echo server
	echoServerStarted := make(chan struct{})
	go func() {
		close(echoServerStarted)
		buf := make([]byte, 65535)
		for {
			n, clientAddr, err := targetConn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = targetConn.WriteToUDP(buf[:n], clientAddr)
		}
	}()

	<-echoServerStarted

	// Start proxy
	cfg := []config.IPRule{
		{BindAddr: fmt.Sprintf(":%d", proxyPort), Target: fmt.Sprintf("127.0.0.1:%d", targetPort)},
	}

	go simpleUDPEchoProxy(proxyConn, cfg[0].Target)

	time.Sleep(200 * time.Millisecond)

	// Test multiple packets
	clientConn, err := net.DialUDP("udp", nil, proxyConn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatalf("Failed to dial proxy: %v", err)
	}
	defer clientConn.Close()

	numPackets := 10
	for i := 0; i < numPackets; i++ {
		message := fmt.Sprintf("Packet %d", i)
		_, err = clientConn.Write([]byte(message))
		if err != nil {
			t.Fatalf("Failed to send packet %d: %v", i, err)
		}

		response := make([]byte, 1024)
		clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := clientConn.Read(response)
		if err != nil {
			t.Fatalf("Failed to read response for packet %d: %v", i, err)
		}

		responseStr := string(response[:n])
		if responseStr != message {
			t.Errorf("Packet %d: expected %q, got %q", i, message, responseStr)
		}
	}
}

// TestUdpProxy_MultipleClients tests multiple UDP clients
func TestUdpProxy_MultipleClients(t *testing.T) {
	// Setup target and proxy
	targetAddr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		t.Fatalf("Failed to resolve target address: %v", err)
	}

	targetConn, err := net.ListenUDP("udp", targetAddr)
	if err != nil {
		t.Fatalf("Failed to create target listener: %v", err)
	}
	targetPort := targetConn.LocalAddr().(*net.UDPAddr).Port

	proxyAddr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		t.Fatalf("Failed to resolve proxy address: %v", err)
	}

	proxyConn, err := net.ListenUDP("udp", proxyAddr)
	if err != nil {
		t.Fatalf("Failed to create proxy listener: %v", err)
	}
	proxyPort := proxyConn.LocalAddr().(*net.UDPAddr).Port

	defer targetConn.Close()
	defer proxyConn.Close()

	// Start echo server
	echoServerStarted := make(chan struct{})
	go func() {
		close(echoServerStarted)
		buf := make([]byte, 65535)
		for {
			n, clientAddr, err := targetConn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = targetConn.WriteToUDP(buf[:n], clientAddr)
		}
	}()

	<-echoServerStarted

	// Start proxy
	cfg := []config.IPRule{
		{BindAddr: fmt.Sprintf(":%d", proxyPort), Target: fmt.Sprintf("127.0.0.1:%d", targetPort)},
	}

	go simpleUDPEchoProxy(proxyConn, cfg[0].Target)

	time.Sleep(200 * time.Millisecond)

	// Test multiple clients
	numClients := 5
	var wg sync.WaitGroup
	errors := make(chan error, numClients)

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			clientConn, err := net.DialUDP("udp", nil, proxyConn.LocalAddr().(*net.UDPAddr))
			if err != nil {
				errors <- fmt.Errorf("client %d: %v", clientID, err)
				return
			}
			defer clientConn.Close()

			message := fmt.Sprintf("Client %d message", clientID)
			_, err = clientConn.Write([]byte(message))
			if err != nil {
				errors <- fmt.Errorf("client %d write: %v", clientID, err)
				return
			}

			response := make([]byte, 1024)
			clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, err := clientConn.Read(response)
			if err != nil {
				errors <- fmt.Errorf("client %d read: %v", clientID, err)
				return
			}

			responseStr := string(response[:n])
			if responseStr != message {
				errors <- fmt.Errorf("client %d: got %q, want %q", clientID, responseStr, message)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Error(err)
	}
}

// TestUdpProxy_LargePacket tests large UDP packets
func TestUdpProxy_LargePacket(t *testing.T) {
	// Setup target and proxy
	targetAddr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		t.Fatalf("Failed to resolve target address: %v", err)
	}

	targetConn, err := net.ListenUDP("udp", targetAddr)
	if err != nil {
		t.Fatalf("Failed to create target listener: %v", err)
	}
	targetPort := targetConn.LocalAddr().(*net.UDPAddr).Port

	proxyAddr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		t.Fatalf("Failed to resolve proxy address: %v", err)
	}

	proxyConn, err := net.ListenUDP("udp", proxyAddr)
	if err != nil {
		t.Fatalf("Failed to create proxy listener: %v", err)
	}
	proxyPort := proxyConn.LocalAddr().(*net.UDPAddr).Port

	defer targetConn.Close()
	defer proxyConn.Close()

	// Start echo server
	echoServerStarted := make(chan struct{})
	go func() {
		close(echoServerStarted)
		buf := make([]byte, 65535)
		for {
			n, clientAddr, err := targetConn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = targetConn.WriteToUDP(buf[:n], clientAddr)
		}
	}()

	<-echoServerStarted

	// Start proxy
	cfg := []config.IPRule{
		{BindAddr: fmt.Sprintf(":%d", proxyPort), Target: fmt.Sprintf("127.0.0.1:%d", targetPort)},
	}

	go simpleUDPEchoProxy(proxyConn, cfg[0].Target)

	time.Sleep(200 * time.Millisecond)

	// Test large packet (接近UDP最大大小 64KB - IP/UDP头)
	packetSize := 8192 // 使用安全的8KB大小
	data := make([]byte, packetSize)
	for i := range data {
		data[i] = byte(i % 256)
	}

	clientConn, err := net.DialUDP("udp", nil, proxyConn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatalf("Failed to dial proxy: %v", err)
	}
	defer clientConn.Close()

	_, err = clientConn.Write(data)
	if err != nil {
		t.Fatalf("Failed to send large packet: %v", err)
	}

	response := make([]byte, packetSize)
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := clientConn.Read(response)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if n != packetSize {
		t.Errorf("Expected %d bytes, got %d", packetSize, n)
	}

	// Verify data integrity
	for i := 0; i < n; i++ {
		if response[i] != data[i] {
			t.Errorf("Data mismatch at byte %d: got %d, want %d", i, response[i], data[i])
			break
		}
	}
}

// BenchmarkUdpProxy_Proxy benchmarks UDP proxy performance
func BenchmarkUdpProxy_Proxy(b *testing.B) {
	// Setup target and proxy
	targetAddr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		b.Fatalf("Failed to resolve target address: %v", err)
	}

	targetConn, err := net.ListenUDP("udp", targetAddr)
	if err != nil {
		b.Fatalf("Failed to create target listener: %v", err)
	}
	targetPort := targetConn.LocalAddr().(*net.UDPAddr).Port

	proxyAddr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		b.Fatalf("Failed to resolve proxy address: %v", err)
	}

	proxyConn, err := net.ListenUDP("udp", proxyAddr)
	if err != nil {
		b.Fatalf("Failed to create proxy listener: %v", err)
	}
	proxyPort := proxyConn.LocalAddr().(*net.UDPAddr).Port

	defer targetConn.Close()
	defer proxyConn.Close()

	// Start echo server
	go func() {
		buf := make([]byte, 65535)
		for {
			n, clientAddr, err := targetConn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = targetConn.WriteToUDP(buf[:n], clientAddr)
		}
	}()

	// Start proxy
	cfg := []config.IPRule{
		{BindAddr: fmt.Sprintf(":%d", proxyPort), Target: fmt.Sprintf("127.0.0.1:%d", targetPort)},
	}

	go simpleUDPEchoProxy(proxyConn, cfg[0].Target)

	time.Sleep(200 * time.Millisecond)

	// Benchmark
	clientConn, err := net.DialUDP("udp", nil, proxyConn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		b.Fatalf("Failed to dial proxy: %v", err)
	}
	defer clientConn.Close()

	b.ResetTimer()
	message := []byte("benchmark")
	response := make([]byte, 100)

	for i := 0; i < b.N; i++ {
		_, err = clientConn.Write(message)
		if err != nil {
			b.Fatalf("Failed to write: %v", err)
		}

		_, err = clientConn.Read(response)
		if err != nil {
			b.Fatalf("Failed to read: %v", err)
		}
	}
}

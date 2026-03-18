package protocol

import (
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/knwgo/yarp/config"
)

// TestNewTcpProxy tests the constructor function
func TestNewTcpProxy(t *testing.T) {
	cfg := []config.IPRule{
		{BindAddr: ":8080", Target: "localhost:8081"},
		{BindAddr: ":8082", Target: "localhost:8083"},
	}

	proxy := NewTcpProxy(cfg)

	if proxy == nil {
		t.Fatal("NewTcpProxy returned nil")
	}

	if len(proxy.cfg) != 2 {
		t.Errorf("Expected 2 rules, got %d", len(proxy.cfg))
	}

	if proxy.cfg[0].BindAddr != ":8080" || proxy.cfg[0].Target != "localhost:8081" {
		t.Errorf("Unexpected first rule: %+v", proxy.cfg[0])
	}
}

// TestTcpProxy_ProxyConnection tests actual TCP connection proxying
func TestTcpProxy_ProxyConnection(t *testing.T) {
	// Find available ports
	targetListener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to create target listener: %v", err)
	}
	targetAddr := targetListener.Addr().String()

	proxyListener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to create proxy listener: %v", err)
	}
	proxyAddr := proxyListener.Addr().String()

	// Close listeners after test
	defer targetListener.Close()
	defer proxyListener.Close()

	// Start a simple echo server on target
	echoServerStarted := make(chan struct{})
	go func() {
		close(echoServerStarted)
		for {
			conn, err := targetListener.Accept()
			if err != nil {
				return
			}
			go handleEchoConnection(conn)
		}
	}()

	<-echoServerStarted

	// Create proxy and handle connections
	cfg := []config.IPRule{
		{BindAddr: proxyAddr, Target: targetAddr},
	}
	proxy := NewTcpProxy(cfg)

	// Start accepting connections
	go func() {
		for {
			conn, err := proxyListener.Accept()
			if err != nil {
				return
			}
			rule := cfg[0]
			go proxy.handleConnection(conn, rule.Target, rule.BindAddr)
		}
	}()

	// Wait a bit for the proxy to start
	time.Sleep(100 * time.Millisecond)

	// Test connection
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("Failed to connect to proxy: %v", err)
	}
	defer conn.Close()

	testMessage := "Hello, TCP Proxy!"
	_, err = conn.Write([]byte(testMessage))
	if err != nil {
		t.Fatalf("Failed to write to connection: %v", err)
	}

	// Read response
	response := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(response)
	if err != nil {
		t.Fatalf("Failed to read from connection: %v", err)
	}

	responseStr := string(response[:n])
	if responseStr != testMessage {
		t.Errorf("Expected response %q, got %q", testMessage, responseStr)
	}
}

// TestTcpProxy_MultipleConnections tests handling multiple concurrent connections
func TestTcpProxy_MultipleConnections(t *testing.T) {
	// Find available ports
	targetListener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to create target listener: %v", err)
	}
	targetAddr := targetListener.Addr().String()

	proxyListener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to create proxy listener: %v", err)
	}
	proxyAddr := proxyListener.Addr().String()

	defer targetListener.Close()
	defer proxyListener.Close()

	// Start echo server
	echoServerStarted := make(chan struct{})
	go func() {
		close(echoServerStarted)
		for {
			conn, err := targetListener.Accept()
			if err != nil {
				return
			}
			go handleEchoConnection(conn)
		}
	}()

	<-echoServerStarted

	// Create proxy
	cfg := []config.IPRule{
		{BindAddr: proxyAddr, Target: targetAddr},
	}
	proxy := NewTcpProxy(cfg)

	go func() {
		for {
			conn, err := proxyListener.Accept()
			if err != nil {
				return
			}
			rule := cfg[0]
			go proxy.handleConnection(conn, rule.Target, rule.BindAddr)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Test multiple concurrent connections
	numConnections := 5
	var wg sync.WaitGroup
	errors := make(chan error, numConnections)

	for i := 0; i < numConnections; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			conn, err := net.Dial("tcp", proxyAddr)
			if err != nil {
				errors <- fmt.Errorf("connection %d: %v", id, err)
				return
			}
			defer conn.Close()

			message := fmt.Sprintf("Message %d", id)
			_, err = conn.Write([]byte(message))
			if err != nil {
				errors <- fmt.Errorf("write %d: %v", id, err)
				return
			}

			response := make([]byte, 1024)
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, err := conn.Read(response)
			if err != nil {
				errors <- fmt.Errorf("read %d: %v", id, err)
				return
			}

			if string(response[:n]) != message {
				errors <- fmt.Errorf("response mismatch %d: got %q, want %q", id, string(response[:n]), message)
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

// TestTcpProxy_LargeData tests proxying large amounts of data
func TestTcpProxy_LargeData(t *testing.T) {
	targetListener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to create target listener: %v", err)
	}
	targetAddr := targetListener.Addr().String()

	proxyListener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to create proxy listener: %v", err)
	}
	proxyAddr := proxyListener.Addr().String()

	defer targetListener.Close()
	defer proxyListener.Close()

	// Start echo server
	echoServerStarted := make(chan struct{})
	go func() {
		close(echoServerStarted)
		for {
			conn, err := targetListener.Accept()
			if err != nil {
				return
			}
			go handleEchoConnection(conn)
		}
	}()

	<-echoServerStarted

	// Create proxy
	cfg := []config.IPRule{
		{BindAddr: proxyAddr, Target: targetAddr},
	}
	proxy := NewTcpProxy(cfg)

	go func() {
		for {
			conn, err := proxyListener.Accept()
			if err != nil {
				return
			}
			rule := cfg[0]
			go proxy.handleConnection(conn, rule.Target, rule.BindAddr)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Test large data transfer
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("Failed to connect to proxy: %v", err)
	}
	defer conn.Close()

	// Create 1MB of data
	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	_, err = conn.Write(data)
	if err != nil {
		t.Fatalf("Failed to write large data: %v", err)
	}

	// Read response
	response := make([]byte, len(data))
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	totalRead := 0
	for totalRead < len(data) {
		n, err := conn.Read(response[totalRead:])
		if err != nil {
			t.Fatalf("Failed to read response: %v", err)
		}
		totalRead += n
	}

	// Verify data
	for i := 0; i < len(data); i++ {
		if response[i] != data[i] {
			t.Errorf("Data mismatch at byte %d: got %d, want %d", i, response[i], data[i])
			break
		}
	}
}

// TestTcpProxy_MultipleRules tests proxy with multiple rules
func TestTcpProxy_MultipleRules(t *testing.T) {
	// Setup two target servers
	targetListener1, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to create target listener 1: %v", err)
	}
	targetAddr1 := targetListener1.Addr().String()

	targetListener2, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to create target listener 2: %v", err)
	}
	targetAddr2 := targetListener2.Addr().String()

	// Setup two proxy listeners
	proxyListener1, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to create proxy listener 1: %v", err)
	}
	proxyAddr1 := proxyListener1.Addr().String()

	proxyListener2, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to create proxy listener 2: %v", err)
	}
	proxyAddr2 := proxyListener2.Addr().String()

	defer targetListener1.Close()
	defer targetListener2.Close()
	defer proxyListener1.Close()
	defer proxyListener2.Close()

	// Start two echo servers
	echoServerStarted1 := make(chan struct{})
	go func() {
		close(echoServerStarted1)
		for {
			conn, err := targetListener1.Accept()
			if err != nil {
				return
			}
			go handleEchoConnection(conn)
		}
	}()

	echoServerStarted2 := make(chan struct{})
	go func() {
		close(echoServerStarted2)
		for {
			conn, err := targetListener2.Accept()
			if err != nil {
				return
			}
			go handleEchoConnection(conn)
		}
	}()

	<-echoServerStarted1
	<-echoServerStarted2

	// Create proxy with multiple rules
	cfg := []config.IPRule{
		{BindAddr: proxyAddr1, Target: targetAddr1},
		{BindAddr: proxyAddr2, Target: targetAddr2},
	}
	proxy := NewTcpProxy(cfg)

	// Start accepting connections for both rules
	go func() {
		for {
			conn, err := proxyListener1.Accept()
			if err != nil {
				return
			}
			rule := cfg[0]
			go proxy.handleConnection(conn, rule.Target, rule.BindAddr)
		}
	}()

	go func() {
		for {
			conn, err := proxyListener2.Accept()
			if err != nil {
				return
			}
			rule := cfg[1]
			go proxy.handleConnection(conn, rule.Target, rule.BindAddr)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Test first rule
	conn1, err := net.Dial("tcp", proxyAddr1)
	if err != nil {
		t.Fatalf("Failed to connect to proxy 1: %v", err)
	}
	defer conn1.Close()

	msg1 := "Test rule 1"
	conn1.Write([]byte(msg1))
	response1 := make([]byte, 1024)
	conn1.SetReadDeadline(time.Now().Add(2 * time.Second))
	n1, _ := conn1.Read(response1)
	if string(response1[:n1]) != msg1 {
		t.Errorf("Rule 1 mismatch: got %q, want %q", string(response1[:n1]), msg1)
	}

	// Test second rule
	conn2, err := net.Dial("tcp", proxyAddr2)
	if err != nil {
		t.Fatalf("Failed to connect to proxy 2: %v", err)
	}
	defer conn2.Close()

	msg2 := "Test rule 2"
	conn2.Write([]byte(msg2))
	response2 := make([]byte, 1024)
	conn2.SetReadDeadline(time.Now().Add(2 * time.Second))
	n2, _ := conn2.Read(response2)
	if string(response2[:n2]) != msg2 {
		t.Errorf("Rule 2 mismatch: got %q, want %q", string(response2[:n2]), msg2)
	}
}

// handleEchoConnection is a helper function that echoes back all data received
func handleEchoConnection(conn net.Conn) {
	defer conn.Close()
	_, err := io.Copy(conn, conn)
	if err != nil {
		return
	}
}

// BenchmarkTcpProxy_Proxy benchmarks the proxy performance
func BenchmarkTcpProxy_Proxy(b *testing.B) {
	targetListener, err := net.Listen("tcp", ":0")
	if err != nil {
		b.Fatalf("Failed to create target listener: %v", err)
	}
	targetAddr := targetListener.Addr().String()

	proxyListener, err := net.Listen("tcp", ":0")
	if err != nil {
		b.Fatalf("Failed to create proxy listener: %v", err)
	}
	proxyAddr := proxyListener.Addr().String()

	defer targetListener.Close()
	defer proxyListener.Close()

	// Start echo server
	go func() {
		for {
			conn, err := targetListener.Accept()
			if err != nil {
				return
			}
			go handleEchoConnection(conn)
		}
	}()

	// Create proxy
	cfg := []config.IPRule{
		{BindAddr: proxyAddr, Target: targetAddr},
	}
	proxy := NewTcpProxy(cfg)

	go func() {
		for {
			conn, err := proxyListener.Accept()
			if err != nil {
				return
			}
			rule := cfg[0]
			go proxy.handleConnection(conn, rule.Target, rule.BindAddr)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Benchmark small message echo
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn, err := net.Dial("tcp", proxyAddr)
		if err != nil {
			b.Fatalf("Failed to connect: %v", err)
		}

		message := []byte("benchmark")
		_, err = conn.Write(message)
		if err != nil {
			conn.Close()
			b.Fatalf("Failed to write: %v", err)
		}

		response := make([]byte, 100)
		_, err = conn.Read(response)
		if err != nil {
			conn.Close()
			b.Fatalf("Failed to read: %v", err)
		}

		conn.Close()
	}
}

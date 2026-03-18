package protocol

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/knwgo/yarp/config"
)

// TestHTTPProxy tests HTTP proxy with real HTTP server
func TestHTTPProxy(t *testing.T) {
	// Create a test HTTP server
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello from backend!"))
	}))
	defer targetServer.Close()

	// Parse target address
	targetURL := targetServer.URL
	if strings.HasPrefix(targetURL, "http://") {
		targetURL = targetURL[7:]
	}

	// Setup proxy listener - force IPv4
	proxyListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create proxy listener: %v", err)
	}
	proxyAddr := proxyListener.Addr().String()
	defer proxyListener.Close()

	// Create HTTP proxy config
	cfg := []config.Http{
		{
			BindAddr: proxyAddr,
			Rules: []config.HostRule{
				{Host: "test.example.com", Target: targetURL},
			},
		},
	}

	proxy := HTTPProxy{Cfg: cfg}

	// Start proxy handler manually
	go func() {
		rules := cfg[0].Rules
		for {
			clientConn, err := proxyListener.Accept()
			if err != nil {
				return
			}
			go proxy.handleConn(clientConn, rules)
		}
	}()

	// Wait for proxy to be ready
	time.Sleep(100 * time.Millisecond)

	// Test HTTP request
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
	}

	req, err := http.NewRequest("GET", "http://test.example.com/", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	// Direct request to proxy
	req.URL.Host = proxyAddr

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	expectedBody := "Hello from backend!"
	if string(body) != expectedBody {
		t.Errorf("Expected body %q, got %q", expectedBody, string(body))
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
}

// TestHTTPProxy_MultipleRules tests HTTP proxy with multiple routing rules
func TestHTTPProxy_MultipleRules(t *testing.T) {
	// Create multiple test servers
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Server 1"))
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Server 2"))
	}))
	defer server2.Close()

	// Parse target addresses
	target1 := server1.URL
	if strings.HasPrefix(target1, "http://") {
		target1 = target1[7:]
	}

	target2 := server2.URL
	if strings.HasPrefix(target2, "http://") {
		target2 = target2[7:]
	}

	// Setup proxy
	proxyListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create proxy listener: %v", err)
	}
	proxyAddr := proxyListener.Addr().String()
	defer proxyListener.Close()

	cfg := []config.Http{
		{
			BindAddr: proxyAddr,
			Rules: []config.HostRule{
				{Host: "server1.example.com", Target: target1},
				{Host: "server2.example.com", Target: target2},
			},
		},
	}

	proxy := HTTPProxy{Cfg: cfg}

	go func() {
		rules := cfg[0].Rules
		for {
			clientConn, err := proxyListener.Accept()
			if err != nil {
				return
			}
			go proxy.handleConn(clientConn, rules)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
	}

	// Test server 1
	req1, _ := http.NewRequest("GET", "http://server1.example.com/", nil)
	req1.URL.Host = proxyAddr
	resp1, err := client.Do(req1)
	if err != nil {
		t.Fatalf("Request to server 1 failed: %v", err)
	}
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()
	if string(body1) != "Server 1" {
		t.Errorf("Server 1: got %q, want %q", string(body1), "Server 1")
	}

	// Test server 2
	req2, _ := http.NewRequest("GET", "http://server2.example.com/", nil)
	req2.URL.Host = proxyAddr
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("Request to server 2 failed: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if string(body2) != "Server 2" {
		t.Errorf("Server 2: got %q, want %q", string(body2), "Server 2")
	}
}

// TestHTTPProxy_WildcardDomain tests wildcard domain matching
func TestHTTPProxy_WildcardDomain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Wildcard match"))
	}))
	defer server.Close()

	target := server.URL
	if strings.HasPrefix(target, "http://") {
		target = target[7:]
	}

	proxyListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create proxy listener: %v", err)
	}
	proxyAddr := proxyListener.Addr().String()
	defer proxyListener.Close()

	cfg := []config.Http{
		{
			BindAddr: proxyAddr,
			Rules: []config.HostRule{
				{Host: "*.example.com", Target: target},
			},
		},
	}

	proxy := HTTPProxy{Cfg: cfg}

	go func() {
		rules := cfg[0].Rules
		for {
			clientConn, err := proxyListener.Accept()
			if err != nil {
				return
			}
			go proxy.handleConn(clientConn, rules)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
	}

	testHosts := []string{"sub1.example.com", "sub2.example.com", "api.example.com"}

	for _, host := range testHosts {
		req, _ := http.NewRequest("GET", "http://"+host+"/", nil)
		req.URL.Host = proxyAddr
		req.Host = host
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request to %s failed: %v", host, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != "Wildcard match" {
			t.Errorf("Host %s: got %q, want %q", host, string(body), "Wildcard match")
		}
	}
}

// TestGetHTTPHeaders tests parsing HTTP headers
func TestGetHTTPHeaders(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		err      bool
	}{
		{
			name:     "simple GET request",
			input:    "GET / HTTP/1.1\r\nHost: example.com\r\n\r\n",
			expected: "GET / HTTP/1.1\r\nHost: example.com\r\n\r\n",
			err:      false,
		},
		{
			name:     "POST request with headers",
			input:    "POST /api HTTP/1.1\r\nHost: api.example.com\r\nContent-Type: application/json\r\n\r\n",
			expected: "POST /api HTTP/1.1\r\nHost: api.example.com\r\nContent-Type: application/json\r\n\r\n",
			err:      false,
		},
		{
			name:  "incomplete request",
			input: "GET / HTTP/1.1\r\nHost: example.com",
			err:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock connection
			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()

			go func() {
				client.Write([]byte(tt.input))
			}()

			bc := newBufConn(server, 8192)
			bc.SetReadDeadline(time.Now().Add(1 * time.Second))

			headers, err := getHTTPHeaders(bc)

			if tt.err {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if string(headers) != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, string(headers))
			}
		})
	}
}

// TestParseHTTPHost tests parsing Host header
func TestParseHTTPHost(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "standard Host header",
			input:    []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"),
			expected: "example.com",
		},
		{
			name:     "Host header with port",
			input:    []byte("GET / HTTP/1.1\r\nHost: example.com:8080\r\n\r\n"),
			expected: "example.com:8080",
		},
		{
			name:     "Host header with whitespace",
			input:    []byte("GET / HTTP/1.1\r\nHost:   example.com   \r\n\r\n"),
			expected: "example.com",
		},
		{
			name:     "no Host header",
			input:    []byte("GET / HTTP/1.1\r\nUser-Agent: test\r\n\r\n"),
			expected: "",
		},
		{
			name:     "mixed case Host header",
			input:    []byte("GET / HTTP/1.1\r\nHOST: EXAMPLE.COM\r\n\r\n"),
			expected: "EXAMPLE.COM",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseHTTPHost(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// TestGetTargetUrl tests target URL resolution
func TestGetTargetUrl(t *testing.T) {
	tests := []struct {
		name      string
		hostPort  string
		rules     []config.HostRule
		expected  string
		wantErr   bool
		wsEnabled bool
	}{
		{
			name:     "exact match",
			hostPort: "example.com",
			rules: []config.HostRule{
				{Host: "example.com", Target: "backend:8080"},
			},
			expected:  "backend:8080",
			wantErr:   false,
			wsEnabled: false,
		},
		{
			name:     "exact match with port",
			hostPort: "example.com:443",
			rules: []config.HostRule{
				{Host: "example.com", Target: "backend:8443"},
			},
			expected:  "backend:8443",
			wantErr:   false,
			wsEnabled: false,
		},
		{
			name:     "wildcard match",
			hostPort: "api.example.com",
			rules: []config.HostRule{
				{Host: "*.example.com", Target: "backend:9000"},
			},
			expected:  "backend:9000",
			wantErr:   false,
			wsEnabled: false,
		},
		{
			name:     "wildcard with sub-subdomain",
			hostPort: "v1.api.example.com",
			rules: []config.HostRule{
				{Host: "*.example.com", Target: "backend:9000"},
			},
			expected:  "backend:9000",
			wantErr:   false,
			wsEnabled: false,
		},
		{
			name:     "websocket enabled",
			hostPort: "ws.example.com",
			rules: []config.HostRule{
				{Host: "ws.example.com", Target: "backend:3000", Ws: boolPtr(true)},
			},
			expected:  "backend:3000",
			wantErr:   false,
			wsEnabled: true,
		},
		{
			name:     "no matching rule",
			hostPort: "unknown.com",
			rules: []config.HostRule{
				{Host: "example.com", Target: "backend:8080"},
			},
			expected:  "",
			wantErr:   true,
			wsEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := getTargetUrl(tt.hostPort, tt.rules)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result.url.Host != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result.url.Host)
			}

			if result.wsEnabled != tt.wsEnabled {
				t.Errorf("WebSocket enabled: expected %v, got %v", tt.wsEnabled, result.wsEnabled)
			}
		})
	}
}

// TestHTTPProxy_ConnectMethod tests CONNECT method (HTTP tunneling)
func TestHTTPProxy_ConnectMethod(t *testing.T) {
	// Setup target listener that properly handles CONNECT
	targetListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create target listener: %v", err)
	}
	targetAddr := targetListener.Addr().String()
	defer targetListener.Close()

	// Start a server that responds to CONNECT
	go func() {
		for {
			conn, err := targetListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				// Read the CONNECT request
				reader := bufio.NewReader(c)
				_, err := http.ReadRequest(reader)
				if err != nil {
					return
				}

				// Respond with 200 Connection Established
				resp := &http.Response{
					StatusCode: http.StatusOK,
					ProtoMajor: 1,
					ProtoMinor: 1,
					Header:     make(http.Header),
				}
				resp.Write(c)

				// Then echo all subsequent data
				io.Copy(c, c)
			}(conn)
		}
	}()

	// Setup proxy
	proxyListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create proxy listener: %v", err)
	}
	proxyAddr := proxyListener.Addr().String()
	defer proxyListener.Close()

	cfg := []config.Http{
		{
			BindAddr: proxyAddr,
			Rules: []config.HostRule{
				{Host: "example.com", Target: targetAddr},
			},
		},
	}

	proxy := HTTPProxy{Cfg: cfg}

	go func() {
		rules := cfg[0].Rules
		for {
			clientConn, err := proxyListener.Accept()
			if err != nil {
				return
			}
			go proxy.handleConn(clientConn, rules)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Test CONNECT method
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("Failed to connect to proxy: %v", err)
	}
	defer conn.Close()

	// Send CONNECT request
	connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", "example.com:443", "example.com")
	_, err = conn.Write([]byte(connectReq))
	if err != nil {
		t.Fatalf("Failed to send CONNECT request: %v", err)
	}

	// Read response
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatalf("Failed to read CONNECT response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Now send data through the tunnel
	testData := "Hello through tunnel"
	_, err = conn.Write([]byte(testData))
	if err != nil {
		t.Fatalf("Failed to write tunnel data: %v", err)
	}

	response := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(response)
	if err != nil {
		t.Fatalf("Failed to read tunnel response: %v", err)
	}

	if string(response[:n]) != testData {
		t.Errorf("Expected %q, got %q", testData, string(response[:n]))
	}
}

// TestHTTPProxy_PostWithBody tests POST requests with body
func TestHTTPProxy_PostWithBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf("Received: %s", string(body))))
	}))
	defer server.Close()

	target := server.URL
	if strings.HasPrefix(target, "http://") {
		target = target[7:]
	}

	proxyListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create proxy listener: %v", err)
	}
	proxyAddr := proxyListener.Addr().String()
	defer proxyListener.Close()

	cfg := []config.Http{
		{
			BindAddr: proxyAddr,
			Rules: []config.HostRule{
				{Host: "post.example.com", Target: target},
			},
		},
	}

	proxy := HTTPProxy{Cfg: cfg}

	go func() {
		rules := cfg[0].Rules
		for {
			clientConn, err := proxyListener.Accept()
			if err != nil {
				return
			}
			go proxy.handleConn(clientConn, rules)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
	}

	testBody := `{"message": "test"}`
	req, _ := http.NewRequest("POST", "http://post.example.com/", bytes.NewBufferString(testBody))
	req.URL.Host = proxyAddr
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	expected := fmt.Sprintf("Received: %s", testBody)
	if string(body) != expected {
		t.Errorf("Expected %q, got %q", expected, string(body))
	}
}

// Helper function
func boolPtr(b bool) *bool {
	return &b
}

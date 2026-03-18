package protocol

import (
	"bytes"
	"context"
	"crypto/tls"
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

// TestHTTPSProxy tests HTTPS proxy with TLS passthrough
func TestHTTPSProxy(t *testing.T) {
	// Create test HTTPS server
	targetServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("HTTPS backend response"))
	}))
	defer targetServer.Close()

	// Parse target address
	targetURL := targetServer.URL
	if strings.HasPrefix(targetURL, "https://") {
		targetURL = targetURL[8:]
	}

	// Setup proxy listener - force IPv4
	proxyListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create proxy listener: %v", err)
	}
	proxyAddr := proxyListener.Addr().String()
	defer proxyListener.Close()

	// Create HTTPS proxy config
	cfg := []config.Http{
		{
			BindAddr: proxyAddr,
			Rules: []config.HostRule{
				{Host: "secure.example.com", Target: targetURL},
			},
		},
	}

	// Start proxy handler manually
	go func() {
		rules := cfg[0].Rules
		for {
			clientConn, err := proxyListener.Accept()
			if err != nil {
				return
			}
			copyConn, sni, err := getHTTPSHostname(clientConn)
			if err != nil {
				clientConn.Close()
				continue
			}

			targetInfo, err := getTargetUrl(sni, rules)
			if err != nil {
				clientConn.Close()
				continue
			}

			ruleKey := fmt.Sprintf("https:%s->%s", sni, targetInfo.url.Host)
			go pipeHostWithStats(copyConn, targetInfo.url.Host, ruleKey)
		}
	}()

	// Wait for proxy to be ready
	time.Sleep(100 * time.Millisecond)

	// Create TLS client that connects to proxy
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("tcp", proxyAddr)
		},
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}

	// Make HTTPS request to proxy
	req, err := http.NewRequest("GET", "https://secure.example.com/", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Host = "secure.example.com"

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to make HTTPS request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	expectedBody := "HTTPS backend response"
	if string(body) != expectedBody {
		t.Errorf("Expected body %q, got %q", expectedBody, string(body))
	}
}

// TestHTTPSProxy_MultipleRules tests HTTPS proxy with multiple routing rules
func TestHTTPSProxy_MultipleRules(t *testing.T) {
	// Create multiple test HTTPS servers
	server1 := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("HTTPS Server 1"))
	}))
	defer server1.Close()

	server2 := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("HTTPS Server 2"))
	}))
	defer server2.Close()

	// Parse target addresses
	target1 := server1.URL
	if strings.HasPrefix(target1, "https://") {
		target1 = target1[8:]
	}

	target2 := server2.URL
	if strings.HasPrefix(target2, "https://") {
		target2 = target2[8:]
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
				{Host: "secure1.example.com", Target: target1},
				{Host: "secure2.example.com", Target: target2},
			},
		},
	}

	go func() {
		rules := cfg[0].Rules
		for {
			clientConn, err := proxyListener.Accept()
			if err != nil {
				return
			}
			copyConn, sni, err := getHTTPSHostname(clientConn)
			if err != nil {
				clientConn.Close()
				continue
			}

			targetInfo, err := getTargetUrl(sni, rules)
			if err != nil {
				clientConn.Close()
				continue
			}

			ruleKey := fmt.Sprintf("https:%s->%s", sni, targetInfo.url.Host)
			go pipeHostWithStats(copyConn, targetInfo.url.Host, ruleKey)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("tcp", proxyAddr)
		},
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}

	// Test server 1
	req1, _ := http.NewRequest("GET", "https://secure1.example.com/", nil)
	req1.Host = "secure1.example.com"
	resp1, err := client.Do(req1)
	if err != nil {
		t.Fatalf("Request to secure1 failed: %v", err)
	}
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()
	if string(body1) != "HTTPS Server 1" {
		t.Errorf("Secure1: got %q, want %q", string(body1), "HTTPS Server 1")
	}

	// Test server 2
	req2, _ := http.NewRequest("GET", "https://secure2.example.com/", nil)
	req2.Host = "secure2.example.com"
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("Request to secure2 failed: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if string(body2) != "HTTPS Server 2" {
		t.Errorf("Secure2: got %q, want %q", string(body2), "HTTPS Server 2")
	}
}

// TestGetHTTPSHostname tests SNI extraction from TLS ClientHello
func TestGetHTTPSHostname(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		wantErr  bool
	}{
		{
			name:    "simple hostname",
			host:    "example.com",
			wantErr: false,
		},
		{
			name:    "hostname with port",
			host:    "example.com:443",
			wantErr: false,
		},
		{
			name:    "subdomain",
			host:    "api.example.com",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a TLS client connection
			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()

			// Client performs TLS handshake with SNI
			go func() {
				tlsClient := tls.Client(client, &tls.Config{
					ServerName: tt.host,
					InsecureSkipVerify: true,
				})
				_ = tlsClient.Handshake()
			}()

			// Extract hostname from server side
			bc, hostname, err := getHTTPSHostname(server)

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

			if hostname != tt.host {
				t.Errorf("Expected hostname %q, got %q", tt.host, hostname)
			}

			if bc == nil {
				t.Errorf("Expected non-nil bufConn")
			}
		})
	}
}

// TestHTTPSProxy_WildcardDomain tests HTTPS proxy with wildcard domain matching
func TestHTTPSProxy_WildcardDomain(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("HTTPS Wildcard"))
	}))
	defer server.Close()

	target := server.URL
	if strings.HasPrefix(target, "https://") {
		target = target[8:]
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
				{Host: "*.https.example.com", Target: target},
			},
		},
	}

	go func() {
		rules := cfg[0].Rules
		for {
			clientConn, err := proxyListener.Accept()
			if err != nil {
				return
			}
			copyConn, sni, err := getHTTPSHostname(clientConn)
			if err != nil {
				clientConn.Close()
				continue
			}

			targetInfo, err := getTargetUrl(sni, rules)
			if err != nil {
				clientConn.Close()
				continue
			}

			ruleKey := fmt.Sprintf("https:%s->%s", sni, targetInfo.url.Host)
			go pipeHostWithStats(copyConn, targetInfo.url.Host, ruleKey)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("tcp", proxyAddr)
		},
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}

	testHosts := []string{"api.https.example.com", "www.https.example.com", "app.https.example.com"}

	for _, host := range testHosts {
		req, _ := http.NewRequest("GET", "https://"+host+"/", nil)
		req.Host = host
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request to %s failed: %v", host, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != "HTTPS Wildcard" {
			t.Errorf("Host %s: got %q, want %q", host, string(body), "HTTPS Wildcard")
		}
	}
}


// TestReadOnlyConn tests the readOnlyConn implementation
func TestReadOnlyConn(t *testing.T) {
	data := []byte("test data")
	reader := bytes.NewReader(data)
	conn := readOnlyConn{reader: reader}

	// Test Read
	buf := make([]byte, 10)
	n, err := conn.Read(buf)
	if err != nil {
		t.Errorf("Read failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("Read returned %d, want %d", n, len(data))
	}
	if string(buf[:n]) != string(data) {
		t.Errorf("Read returned wrong data")
	}

	// Test Write
	_, err = conn.Write([]byte("test"))
	if err != io.ErrClosedPipe {
		t.Errorf("Write should return ErrClosedPipe, got %v", err)
	}

	// Test Close
	err = conn.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Test other methods (should return nil)
	if conn.LocalAddr() != nil {
		t.Error("LocalAddr should return nil")
	}
	if conn.RemoteAddr() != nil {
		t.Error("RemoteAddr should return nil")
	}
	if err := conn.SetDeadline(time.Now()); err != nil {
		t.Errorf("SetDeadline failed: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now()); err != nil {
		t.Errorf("SetReadDeadline failed: %v", err)
	}
	if err := conn.SetWriteDeadline(time.Now()); err != nil {
		t.Errorf("SetWriteDeadline failed: %v", err)
	}
}

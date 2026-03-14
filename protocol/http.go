package protocol

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"time"

	"golang.org/x/sync/errgroup"
	"k8s.io/klog/v2"

	"github.com/knwgo/yarp/config"
)

type HTTPProxy struct {
	Cfg []config.Http
}

func (hp HTTPProxy) Start() error {
	hf := func(ba string, rules []config.HostRule) error {
		ln, err := net.Listen("tcp", ba)
		if err != nil {
			return err
		}

		for {
			clientConn, err := ln.Accept()
			if err != nil {
				klog.Errorf("failed to accept client connection: %v", err)
				continue
			}

			go hp.handleConn(clientConn, rules)
		}
	}

	eg := errgroup.Group{}

	for _, ch := range hp.Cfg {
		ch := ch
		eg.Go(func() error {
			return hf(ch.BindAddr, ch.Rules)
		})
	}

	return eg.Wait()
}

func (hp HTTPProxy) handleConn(clientConn net.Conn, rules []config.HostRule) {
	bc := newBufConn(clientConn, 8192)

	data, err := getHTTPHeaders(bc)
	if err != nil {
		klog.Errorf("get http host error: %v", err)
		_ = clientConn.Close()
		return
	}

	host := parseHTTPHost(data)
	if host == "" {
		klog.Errorf("[http] no host header found")
		_ = clientConn.Close()
		return
	}

	targetInfo, err := getTargetUrl(host, rules)
	if err != nil {
		klog.Errorf("[http] %s form %s get target url error: %v", host, clientConn.RemoteAddr(), err)
		_ = clientConn.Close()
		return
	}

	targetHost := targetInfo.url.Host
	wsEnabled := targetInfo.wsEnabled
	ruleKey := fmt.Sprintf("http:%s->%s", host, targetHost)

	// Check if WebSocket upgrade is requested
	if wsEnabled && isWebSocketRequest(data) {
		klog.Infof("[ws] new conn from: %s, %s -> %s", clientConn.RemoteAddr(), host, targetHost)
		bc.Unread(data)
		go handleWsConnection(bc, targetHost, ruleKey)
		return
	}

	klog.Infof("[http] new conn from: %s, %s -> %s", clientConn.RemoteAddr(), host, targetHost)
	bc.Unread(data)
	go pipeHostWithStats(bc, targetHost, ruleKey)
}

func getHTTPHost(conn *bufConn) (string, error) {
	data, err := getHTTPHeaders(conn)
	if err != nil {
		return "", err
	}

	firstLineEnd := bytes.Index(data, []byte("\r\n"))
	if firstLineEnd == -1 {
		return "", fmt.Errorf("invalid http request: no CRLF")
	}
	firstLine := data[:firstLineEnd]

	fields := bytes.Split(firstLine, []byte(" "))
	if len(fields) >= 2 && bytes.EqualFold(fields[0], []byte("CONNECT")) {
		target := string(fields[1])
		conn.Unread(data)
		return target, nil
	}

	host := parseHTTPHost(data)
	if host == "" {
		conn.Unread(data)
		return "", fmt.Errorf("no host header found")
	}

	conn.Unread(data)
	return host, nil
}

func getHTTPHeaders(conn *bufConn) ([]byte, error) {
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	defer func() {
		_ = conn.SetReadDeadline(time.Time{})
	}()

	var (
		headerBuf bytes.Buffer
		tmp       = make([]byte, 1024)
	)

	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			headerBuf.Write(tmp[:n])
			if bytes.Contains(headerBuf.Bytes(), []byte("\r\n\r\n")) {
				break
			}
		}

		if err != nil {
			if err == io.EOF && headerBuf.Len() > 0 {
				break
			}
			return nil, fmt.Errorf("read http header error: %v", err)
		}

		// 128KB
		if headerBuf.Len() > 128*1024 {
			return nil, fmt.Errorf("header too large")
		}
	}

	return headerBuf.Bytes(), nil
}

func parseHTTPHost(b []byte) string {
	lines := bytes.Split(b, []byte("\r\n"))
	for _, line := range lines {
		l := bytes.ToLower(line)
		if bytes.HasPrefix(l, []byte("host:")) {
			host := bytes.TrimSpace(line[len("host:"):])
			return string(host)
		}
	}
	return ""
}

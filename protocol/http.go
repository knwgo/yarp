package protocol

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"time"

	"k8s.io/klog/v2"

	"github.com/knwgo/yarp/config"
)

type HTTPProxy struct {
	Cfg config.Http
}

func (hp HTTPProxy) Start() error {
	ln, err := net.Listen("tcp", hp.Cfg.BindAddr)
	if err != nil {
		return err
	}

	for {
		clientConn, err := ln.Accept()
		if err != nil {
			klog.Errorf("failed to accept client connection: %v", err)
			continue
		}

		go hp.handleConn(clientConn)
	}
}

func (hp HTTPProxy) handleConn(clientConn net.Conn) {
	bc := newBufConn(clientConn, 8192)

	host, err := getHTTPHost(bc)
	if err != nil {
		klog.Errorf("get http host error: %v", err)
		_ = clientConn.Close()
		return
	}

	targetHost, err := getTargetUrl(host, hp.Cfg.Rules)
	if err != nil {
		klog.Errorf("[http] %s get target url error: %v", host, err)
		_ = clientConn.Close()
		return
	}

	klog.Infof("[http] new conn from: %s, %s -> %s", clientConn.RemoteAddr(), host, targetHost)

	ruleKey := fmt.Sprintf("http:%s->%s", host, targetHost.Host)
	go pipeHostWithStats(bc, targetHost.Host, ruleKey)
}

func getHTTPHost(conn *bufConn) (string, error) {
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
			return "", fmt.Errorf("read http header error: %v", err)
		}

		// 128KB
		if headerBuf.Len() > 128*1024 {
			return "", fmt.Errorf("header too large")
		}
	}

	data := headerBuf.Bytes()

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

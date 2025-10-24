package protocol

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"k8s.io/klog/v2"

	"github.com/knwgo/yarp/config"
)

type HTTPSProxy struct {
	Cfg config.Http
}

func (hp HTTPSProxy) Start() error {
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

		copyConn, sni, err := getHTTPSHostname(clientConn)
		if err != nil {
			klog.Errorf("get https hostname error: %v", err)
			_ = clientConn.Close()
			continue
		}

		targetHost, err := getTargetUrl(sni, hp.Cfg.Rules)
		if err != nil {
			klog.Errorf("[https] %s from %s get target url error: %v", sni, clientConn.RemoteAddr(), err)
			_ = clientConn.Close()
			continue
		}

		klog.Infof("[https] new conn from: %s, %s -> %s", clientConn.RemoteAddr(), sni, targetHost.Host)

		ruleKey := fmt.Sprintf("https:%s->%s", sni, targetHost.Host)
		go pipeHostWithStats(copyConn, targetHost.Host, ruleKey)
	}
}

func getHTTPSHostname(conn net.Conn) (*bufConn, string, error) {
	bc := newBufConn(conn, 8192)

	header := make([]byte, 5)
	if _, err := io.ReadFull(bc, header); err != nil {
		return nil, "", fmt.Errorf("read TLS header failed: %w", err)
	}
	if header[0] != 0x16 { // Handshake
		return nil, "", fmt.Errorf("not TLS handshake record, got 0x%x", header[0])
	}

	recordLen := int(binary.BigEndian.Uint16(header[3:5]))
	if recordLen <= 0 || recordLen > 128*1024 {
		return nil, "", fmt.Errorf("invalid TLS record length: %d", recordLen)
	}

	body := make([]byte, recordLen)
	if _, err := io.ReadFull(bc, body); err != nil {
		return nil, "", fmt.Errorf("read TLS body failed: %w", err)
	}

	clientHelloData := append(header, body...)

	buf := bytes.NewBuffer(nil)
	tee := io.TeeReader(bytes.NewReader(clientHelloData), buf)
	var hello *tls.ClientHelloInfo
	readOnly := readOnlyConn{reader: tee}

	_ = tls.Server(readOnly, &tls.Config{
		GetConfigForClient: func(info *tls.ClientHelloInfo) (*tls.Config, error) {
			hello = &tls.ClientHelloInfo{}
			*hello = *info
			return nil, nil
		},
	}).Handshake()

	if hello == nil || hello.ServerName == "" {
		return nil, "", fmt.Errorf("failed to get SNI")
	}

	bc.Unread(clientHelloData)

	return bc, hello.ServerName, nil
}

type readOnlyConn struct {
	reader io.Reader
}

func (r readOnlyConn) Read(p []byte) (int, error)         { return r.reader.Read(p) }
func (r readOnlyConn) Write(_ []byte) (int, error)        { return 0, io.ErrClosedPipe }
func (r readOnlyConn) Close() error                       { return nil }
func (r readOnlyConn) LocalAddr() net.Addr                { return nil }
func (r readOnlyConn) RemoteAddr() net.Addr               { return nil }
func (r readOnlyConn) SetDeadline(_ time.Time) error      { return nil }
func (r readOnlyConn) SetReadDeadline(_ time.Time) error  { return nil }
func (r readOnlyConn) SetWriteDeadline(_ time.Time) error { return nil }

package protocol

import (
	"bytes"
	"crypto/tls"
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

		copyClientConn, host, err := getHTTPSHostname(clientConn)
		if err != nil {
			klog.Errorf("get https hostname error: %v", err)
			_ = clientConn.Close()
			continue
		}

		targetHost, err := getTargetUrl(host, hp.Cfg.Rules)
		if err != nil {
			klog.Errorf("get target url error: %v", err)
			continue
		}
		targetHost.Scheme = "https"
		klog.Infof("[https] new conn from: %s, %s -> %s", clientConn.RemoteAddr(), host, targetHost)

		go hp.handleClientConn(copyClientConn, targetHost.Host)
	}
}

func (hp HTTPSProxy) handleClientConn(cliConn net.Conn, targetHost string) {
	targetConn, err := net.Dial("tcp", targetHost)
	if err != nil {
		klog.Errorf("dial target host error: %v", err)
		return
	}
	if err := pipe(cliConn, targetConn); err != nil {
		klog.Errorf("pipe target host error: %v", err)
	}
}

func getHTTPSHostname(c net.Conn) (net.Conn, string, error) {
	sc, rd := newSharedConn(c)

	clientHello, err := readClientHello(rd)
	if err != nil {
		return nil, "", err
	}

	return sc, clientHello.ServerName, nil
}

func readClientHello(reader io.Reader) (*tls.ClientHelloInfo, error) {
	var hello *tls.ClientHelloInfo

	err := tls.Server(readOnlyConn{reader: reader}, &tls.Config{
		GetConfigForClient: func(argHello *tls.ClientHelloInfo) (*tls.Config, error) {
			hello = &tls.ClientHelloInfo{}
			*hello = *argHello
			return nil, nil
		},
	}).Handshake()

	if hello == nil {
		return nil, err
	}
	return hello, nil
}

type readOnlyConn struct {
	reader io.Reader
}

func (conn readOnlyConn) Read(p []byte) (int, error)         { return conn.reader.Read(p) }
func (conn readOnlyConn) Write(_ []byte) (int, error)        { return 0, io.ErrClosedPipe }
func (conn readOnlyConn) Close() error                       { return nil }
func (conn readOnlyConn) LocalAddr() net.Addr                { return nil }
func (conn readOnlyConn) RemoteAddr() net.Addr               { return nil }
func (conn readOnlyConn) SetDeadline(_ time.Time) error      { return nil }
func (conn readOnlyConn) SetReadDeadline(_ time.Time) error  { return nil }
func (conn readOnlyConn) SetWriteDeadline(_ time.Time) error { return nil }

type sharedConn struct {
	net.Conn
	buf *bytes.Buffer
}

func newSharedConn(conn net.Conn) (*sharedConn, io.Reader) {
	sc := &sharedConn{
		Conn: conn,
		buf:  bytes.NewBuffer(make([]byte, 0, 1024)),
	}
	return sc, io.TeeReader(conn, sc.buf)
}

func (sc sharedConn) Read(p []byte) (int, error) {
	if sc.buf == nil {
		return sc.Conn.Read(p)
	}
	n, err := sc.buf.Read(p)
	if err == io.EOF {
		sc.buf = nil
		var n2 int
		n2, err = sc.Conn.Read(p[n:])
		n += n2
	}
	return n, err
}

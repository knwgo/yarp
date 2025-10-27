package protocol

import (
	"io"
	"net"
	"sync/atomic"
	"time"

	"k8s.io/klog/v2"

	"github.com/knwgo/yarp/stat"
)

func pipe(src net.Conn, dest net.Conn) error {
	errChan := make(chan error, 1)
	onClose := func(err error) {
		_ = dest.Close()
		_ = src.Close()
	}
	go func() {
		_, err := io.Copy(src, dest)
		errChan <- err
		onClose(err)
	}()
	go func() {
		_, err := io.Copy(dest, src)
		errChan <- err
		onClose(err)
	}()
	return <-errChan
}

func pipeHost(src net.Conn, targetHost string) {
	targetConn, err := net.Dial("tcp", targetHost)
	if err != nil {
		klog.Errorf("dial target host error: %v", err)
		return
	}

	_ = targetConn.SetDeadline(time.Time{})
	_ = src.SetDeadline(time.Time{})

	if err := pipe(src, targetConn); err != nil {
		klog.Errorf("pipe target host error: %v", err)
	}
}

type countingWriter struct {
	io.Writer
	count       *int64
	buffered    int64
	bufferLimit int64
	onWrite     func(n int64)
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	n, err := cw.Writer.Write(p)
	if n > 0 {
		atomic.AddInt64(cw.count, int64(n))
		cw.buffered += int64(n)
		if cw.buffered >= cw.bufferLimit && cw.onWrite != nil {
			cw.onWrite(cw.buffered)
			cw.buffered = 0
		}
	}
	return n, err
}

func pipeWithStats(src net.Conn, dest net.Conn, ruleKey string) error {
	stat.GlobalStats.AddConn(ruleKey)
	defer stat.GlobalStats.RemoveConn(ruleKey)

	defer func() {
		_ = dest.Close()
		_ = src.Close()
	}()

	var bytesSrcToDest, bytesDestToSrc int64
	errChan := make(chan error, 2)

	countingWriterWithStats := func(writer io.Writer, count *int64, isSrcToDest bool) io.Writer {
		return &countingWriter{
			Writer:      writer,
			count:       count,
			bufferLimit: 2 * 1024,
			onWrite: func(n int64) {
				if isSrcToDest {
					stat.GlobalStats.AddBytes(ruleKey, 0, n)
				} else {
					stat.GlobalStats.AddBytes(ruleKey, n, 0)
				}
			},
		}
	}

	go func() {
		_, err := io.Copy(countingWriterWithStats(dest, &bytesSrcToDest, true), src)
		errChan <- err
	}()
	go func() {
		_, err := io.Copy(countingWriterWithStats(src, &bytesDestToSrc, false), dest)
		errChan <- err
	}()

	return <-errChan
}

func pipeHostWithStats(src net.Conn, targetHost, ruleKey string) {
	targetConn, err := net.Dial("tcp", targetHost)
	if err != nil {
		klog.Errorf("dial target host error: %v", err)
		return
	}

	_ = targetConn.SetDeadline(time.Time{})
	_ = src.SetDeadline(time.Time{})

	if err := pipeWithStats(src, targetConn, ruleKey); err != nil {
		klog.Errorf("pipe target host error: %v", err)
	}
}

package protocol

import (
	"io"
	"net"
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

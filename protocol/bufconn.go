package protocol

import (
	"bufio"
	"bytes"
	"io"
	"net"
)

type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func newBufConn(c net.Conn, size int) *bufConn {
	return &bufConn{
		Conn: c,
		r:    bufio.NewReaderSize(c, size),
	}
}

func (b *bufConn) Read(p []byte) (int, error) {
	return b.r.Read(p)
}

func (b *bufConn) Reader() *bufio.Reader {
	return b.r
}

func (b *bufConn) Unread(p []byte) {
	if len(p) == 0 {
		return
	}

	if remaining := b.r.Buffered(); remaining > 0 {
		buf, _ := b.r.Peek(remaining)
		b.r.Reset(io.MultiReader(bytes.NewReader(p), bytes.NewReader(buf), b.Conn))
	} else {
		b.r.Reset(io.MultiReader(bytes.NewReader(p), b.Conn))
	}
}

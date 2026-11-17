// Package podtoast implements PodToast's versioned API over an authenticated
// Kubernetes exec byte stream. No listener or credentials live in the pod.
package podtoast

import (
	"io"
	"net"
	"sync"
	"time"

	"golang.org/x/net/http2"
	"net/http"
)

const Version = "1"

// StreamConn adapts exec/stdin/stdout to HTTP/2. HTTP/2 supplies independent
// stream cancellation, flow control and bounded frames for concurrent work.
type StreamConn struct {
	io.Reader
	io.Writer
	CloseFunc func() error
	once      sync.Once
}

func (c *StreamConn) Close() error {
	var err error
	c.once.Do(func() {
		if c.CloseFunc != nil {
			err = c.CloseFunc()
		}
	})
	return err
}
func (*StreamConn) LocalAddr() net.Addr              { return addr("podtoast") }
func (*StreamConn) RemoteAddr() net.Addr             { return addr("exec") }
func (*StreamConn) SetDeadline(time.Time) error      { return nil }
func (*StreamConn) SetReadDeadline(time.Time) error  { return nil }
func (*StreamConn) SetWriteDeadline(time.Time) error { return nil }

type addr string

func (a addr) Network() string { return "exec" }
func (a addr) String() string  { return string(a) }

func Serve(conn net.Conn, handler http.Handler) {
	defer conn.Close()
	server := http2.Server{MaxConcurrentStreams: 32, MaxReadFrameSize: 64 << 10,
		MaxUploadBufferPerConnection: 2 << 20, MaxUploadBufferPerStream: 256 << 10,
		IdleTimeout: 30 * time.Minute}
	server.ServeConn(conn, &http2.ServeConnOpts{Handler: handler})
}

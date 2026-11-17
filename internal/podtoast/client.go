package podtoast

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"golang.org/x/net/http2"
)

type Client struct {
	lifetime         sync.Mutex
	draining         bool
	leases, requests int
	// Set by the transport owner before sharing the connection.
	AgentName, ImageRef string
	directorySummary    bool
	cc                  *http2.ClientConn
	conn                net.Conn
}

func Connect(ctx context.Context, conn net.Conn) (*Client, error) {
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	cc, err := (&http2.Transport{}).NewClientConn(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	c := &Client{cc: cc, conn: conn}
	var cap Capability
	if err = c.JSON(ctx, "GET", "/v1/hello", nil, &cap); err != nil {
		c.Close()
		return nil, err
	}
	if cap.Version != Version || (!cap.Files && !cap.Traffic) {
		c.Close()
		return nil, fmt.Errorf("unsupported PodToast protocol %q", cap.Version)
	}
	c.directorySummary = cap.DirectorySummary
	return c, nil
}
func (c *Client) Close() error { _ = c.cc.Close(); return c.conn.Close() }
func (c *Client) Healthy() bool {
	c.lifetime.Lock()
	defer c.lifetime.Unlock()
	return !c.draining && c.cc.CanTakeNewRequest()
}

// Drain prevents new adoption, but retains existing filesystem borrowers and
// HTTP streams until they finish. It never aborts an in-flight copy.
func (c *Client) Drain() {
	c.lifetime.Lock()
	c.draining = true
	closeNow := c.leases == 0 && c.requests == 0
	c.lifetime.Unlock()
	if closeNow {
		c.Close()
	}
}
func (c *Client) release(request bool) {
	c.lifetime.Lock()
	if request {
		c.requests--
	} else {
		c.leases--
	}
	closeNow := c.draining && c.leases == 0 && c.requests == 0
	c.lifetime.Unlock()
	if closeNow {
		c.Close()
	}
}

type trackedBody struct {
	io.ReadCloser
	once sync.Once
	c    *Client
}

func (b *trackedBody) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	if e != nil {
		b.once.Do(func() { b.c.release(true) })
	}
	return n, e
}
func (b *trackedBody) Close() error {
	e := b.ReadCloser.Close()
	b.once.Do(func() { b.c.release(true) })
	return e
}

func (c *Client) request(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://podtoast"+path, body)
	if err != nil {
		return nil, err
	}
	c.lifetime.Lock()
	c.requests++
	c.lifetime.Unlock()
	resp, err := c.cc.RoundTrip(req)
	if err != nil {
		c.release(true)
		return nil, err
	}
	resp.Body = &trackedBody{ReadCloser: resp.Body, c: c}
	if resp.StatusCode != 200 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		var cause error
		switch resp.StatusCode {
		case 404:
			cause = os.ErrNotExist
		case 403:
			cause = os.ErrPermission
		case 409:
			cause = os.ErrExist
		default:
			cause = errors.New(string(b))
		}
		return nil, &os.PathError{Op: "PodToast " + path, Err: cause}
	}
	return resp, nil
}
func (c *Client) JSON(ctx context.Context, method, path string, in, out any) error {
	var b io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		b = bytes.NewReader(raw)
	}
	r, err := c.request(ctx, method, path, b)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if out == nil {
		_, err = io.Copy(io.Discard, r.Body)
		return err
	}
	return json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(out)
}
func (c *Client) Monitor(ctx context.Context, fn func(Snapshot) error) error {
	r, err := c.request(ctx, "GET", "/v1/monitor", nil)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	d := json.NewDecoder(r.Body)
	for {
		var s Snapshot
		if err = d.Decode(&s); err != nil {
			return err
		}
		if err = fn(s); err != nil {
			return err
		}
	}
}
func (c *Client) FS(ctx context.Context) *FS {
	c.lifetime.Lock()
	c.leases++
	c.lifetime.Unlock()
	return &FS{c: c, ctx: ctx}
}

type FS struct {
	closeOnce sync.Once
	c         *Client
	ctx       context.Context
}

func (f *FS) Close() error { f.closeOnce.Do(func() { f.c.release(false) }); return nil }
func (f *FS) op(o Operation) (Result, error) {
	o.RawPath, o.RawTo, o.RawPrevious = []byte(o.Path), []byte(o.To), []byte(o.Previous)
	var r Result
	err := f.c.JSON(f.ctx, "POST", "/v1/op", o, &r)
	if r.RawText != nil {
		r.Text = string(r.RawText)
	}
	return r, err
}
func (f *FS) ReadDir(p string) ([]os.FileInfo, error) {
	r, err := f.c.request(f.ctx, "GET", "/v1/list?links=1&path="+url.QueryEscape(p), nil)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	var entries []os.FileInfo
	d := json.NewDecoder(r.Body)
	for {
		var row struct {
			Result
			Error string `json:"error"`
		}
		if err = d.Decode(&row); err != nil {
			return nil, err
		}
		if row.Error != "" {
			return nil, errors.New(row.Error)
		}
		if row.Text == "done" {
			return entries, nil
		}
		if row.Entry == nil {
			return nil, errors.New("invalid PodToast directory record")
		}
		if len(entries) >= 100000 {
			return nil, fmt.Errorf("remote folder exceeds 100000 entries")
		}
		entries = append(entries, *row.Entry)
	}
}
func (f *FS) Lstat(p string) (os.FileInfo, error) {
	r, e := f.op(Operation{Op: "lstat", Path: p})
	if e != nil {
		return nil, e
	}
	if r.Entry == nil {
		return nil, errors.New("missing file info")
	}
	return *r.Entry, nil
}
func (f *FS) Stat(p string) (os.FileInfo, error) {
	r, e := f.op(Operation{Op: "stat", Path: p})
	if e != nil {
		return nil, e
	}
	if r.Entry == nil {
		return nil, errors.New("missing file info")
	}
	return *r.Entry, nil
}
func (f *FS) Open(p string) (io.ReadCloser, error) {
	r, e := f.c.request(f.ctx, "GET", "/v1/read?path="+url.QueryEscape(p), nil)
	if e != nil {
		return nil, e
	}
	return &reader{Response: r}, nil
}

type reader struct{ *http.Response }

func (r *reader) Read(p []byte) (int, error) {
	n, e := r.Body.Read(p)
	if e == io.EOF && r.Trailer.Get("Podtoast-Error") != "" {
		e = errors.New(r.Trailer.Get("Podtoast-Error"))
	}
	return n, e
}
func (r *reader) Close() error { return r.Body.Close() }
func (f *FS) Create(p string) (io.WriteCloser, error) {
	ctx, cancel := context.WithCancel(f.ctx)
	r, w := io.Pipe()
	out := &writer{PipeWriter: w, cancel: cancel, done: make(chan struct{})}
	go func() {
		resp, err := f.c.request(ctx, "PUT", "/v1/write?path="+url.QueryEscape(p), r)
		if resp != nil {
			_, readErr := io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if err == nil {
				err = readErr
			}
		}
		r.CloseWithError(err)
		out.err = err
		close(out.done)
	}()
	return out, nil
}

type writer struct {
	*io.PipeWriter
	cancel context.CancelFunc
	done   chan struct{}
	closed bool
	err    error
}

func (w *writer) Write(p []byte) (int, error) {
	n, err := w.PipeWriter.Write(p)
	if err != nil {
		<-w.done
		if w.err != nil {
			return n, w.err
		}
	}
	return n, err
}
func (w *writer) Close() error {
	if w.closed {
		return w.err
	}
	w.closed = true
	_ = w.PipeWriter.Close()
	<-w.done
	w.cancel()
	return w.err
}
func (f *FS) Mkdir(p string) error { _, e := f.op(Operation{Op: "mkdir", Path: p}); return e }
func (f *FS) Remove(p string, d bool) error {
	_, e := f.op(Operation{Op: "remove", Path: p, Directory: d})
	return e
}
func (f *FS) Rename(p, to string) error {
	_, e := f.op(Operation{Op: "rename", Path: p, To: to})
	return e
}
func (f *FS) Chmod(p string, m os.FileMode) error {
	_, e := f.op(Operation{Op: "chmod", Path: p, Mode: m})
	return e
}
func (f *FS) Chtimes(p string, t time.Time) error {
	_, e := f.op(Operation{Op: "chtimes", Path: p, Time: t})
	return e
}
func (f *FS) Readlink(p string) (string, error) {
	r, e := f.op(Operation{Op: "readlink", Path: p})
	return r.Text, e
}
func (f *FS) Symlink(target, p string) error {
	_, e := f.op(Operation{Op: "symlink", Path: target, To: p})
	return e
}
func (f *FS) Canonical(p string) (string, error) {
	r, e := f.op(Operation{Op: "canonical", Path: p})
	return r.Text, e
}
func (f *FS) CopyFile(p, to, previous string) error {
	_, e := f.op(Operation{Op: "copy", Path: p, To: to, Previous: previous})
	return e
}

// Metrics returns bounded Prometheus text over the authenticated exec stream.
func (c *Client) Metrics(ctx context.Context) ([]byte, error) {
	r, err := c.request(ctx, "GET", "/metrics", nil)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metrics: HTTP %d", r.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
	if len(b) > 1<<20 {
		return nil, fmt.Errorf("metrics response exceeds 1 MiB")
	}
	return b, err
}

package kube

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
)

// ContainerRef names one container of a pod.
type ContainerRef struct {
	Namespace, Pod, Container string
}

// ExecOptions describes one command run in a container. Stdin, Stdout and
// Stderr may be nil; with TTY the server merges stderr into stdout and Resize
// delivers terminal size changes.
type ExecOptions struct {
	Command []string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	TTY     bool
	Resize  <-chan remotecommand.TerminalSize
}

// Executor runs a command in a container and returns when it ends. A non-zero
// exit status is reported as a *utilexec.CodeExitError (see ExitCode). Tests
// inject an Executor backed by a local shell.
type Executor interface {
	Stream(ctx context.Context, ref ContainerRef, opts ExecOptions) error
}

type apiExecutor struct {
	cfg  *rest.Config
	core kubernetes.Interface
}

// NewExecutor returns the API-server executor: WebSocket (as kubectl does
// since 1.30) with a fallback to SPDY when the upgrade is refused.
func NewExecutor(cfg *rest.Config, core kubernetes.Interface) Executor {
	return &apiExecutor{cfg: cfg, core: core}
}

func (x *apiExecutor) Stream(ctx context.Context, ref ContainerRef, opts ExecOptions) error {
	req := x.core.CoreV1().RESTClient().Post().Resource("pods").Namespace(ref.Namespace).Name(ref.Pod).SubResource("exec")
	req.VersionedParams(&corev1.PodExecOptions{
		Container: ref.Container,
		Command:   opts.Command,
		Stdin:     opts.Stdin != nil,
		Stdout:    opts.Stdout != nil,
		Stderr:    opts.Stderr != nil && !opts.TTY,
		TTY:       opts.TTY,
	}, scheme.ParameterCodec)
	return x.streamURL(ctx, req.URL(), opts)
}
func (x *apiExecutor) streamURL(ctx context.Context, endpoint *url.URL, opts ExecOptions) error {
	cfg, endpoint, cleanup, err := streamDialBridge(ctx, x.cfg, endpoint)
	if err != nil {
		return err
	}
	defer cleanup()
	spdy, err := remotecommand.NewSPDYExecutor(cfg, http.MethodPost, endpoint)
	if err != nil {
		return err
	}
	ws, err := remotecommand.NewWebSocketExecutor(cfg, http.MethodGet, endpoint.String())
	if err != nil {
		return err
	}
	executor, err := remotecommand.NewFallbackExecutor(ws, spdy, func(err error) bool {
		return httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err)
	})
	if err != nil {
		return err
	}
	var queue remotecommand.TerminalSizeQueue
	if opts.TTY && opts.Resize != nil {
		queue = &sizeQueue{ctx: ctx, sizes: opts.Resize}
	}
	return executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             opts.Stdin,
		Stdout:            opts.Stdout,
		Stderr:            opts.Stderr,
		Tty:               opts.TTY,
		TerminalSizeQueue: queue,
	})
}

// sizeQueue adapts a channel of sizes to remotecommand.TerminalSizeQueue.
type sizeQueue struct {
	ctx   context.Context
	sizes <-chan remotecommand.TerminalSize
}

func (q *sizeQueue) Next() *remotecommand.TerminalSize {
	select {
	case s, ok := <-q.sizes:
		if !ok {
			return nil
		}
		return &s
	case <-q.ctx.Done():
		return nil
	}
}

// ExitCode extracts the exit status of a finished command, if the error
// carries one.
func ExitCode(err error) (int, bool) {
	var code utilexec.CodeExitError
	if errors.As(err, &code) {
		return code.Code, true
	}
	var codePtr *utilexec.CodeExitError
	if errors.As(err, &codePtr) {
		return codePtr.Code, true
	}
	return 0, false
}

// Executor returns the exec transport of this client.
func (c *Client) Executor() Executor { return NewExecutor(c.streamConfig, c.stream) }

// Attacher streams the existing ephemeral-container entrypoint; it does not
// start a second shell or override the chosen image command.
func (c *Client) Attacher(tty bool) Executor {
	return &apiAttacher{apiExecutor: &apiExecutor{cfg: c.streamConfig, core: c.stream}, tty: tty}
}

type apiAttacher struct {
	*apiExecutor
	tty bool
}

func (x *apiAttacher) Stream(ctx context.Context, ref ContainerRef, opts ExecOptions) error {
	opts.TTY = x.tty
	req := x.core.CoreV1().RESTClient().Post().Resource("pods").Namespace(ref.Namespace).Name(ref.Pod).SubResource("attach")
	req.VersionedParams(&corev1.PodAttachOptions{Container: ref.Container, Stdin: true, Stdout: true, Stderr: !x.tty, TTY: x.tty}, scheme.ParameterCodec)
	if !x.tty {
		w := opts.Stdout
		if w == nil {
			w = io.Discard
		}
		combined := &serialAttachWriter{w: w}
		opts.Stdout, opts.Stderr = combined, combined
	}
	return x.streamURL(ctx, req.URL(), opts)
}

type serialAttachWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (w *serialAttachWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

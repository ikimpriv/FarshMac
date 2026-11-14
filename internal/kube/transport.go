package kube

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// contextualConfig also cancels APIs in client-go / kubectl which do not
// accept a context (discovery and describers). Each operation gets its own
// wrapper; cancelling one operation never cancels another one's requests.
func contextualConfig(cfg *rest.Config, ctx context.Context) *rest.Config {
	copy := rest.CopyConfig(cfg)
	copy.Wrap(func(next http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requestCtx, cancel := context.WithCancel(r.Context())
			stop := context.AfterFunc(ctx, cancel)
			if ctx.Err() != nil {
				cancel()
			}
			response, err := next.RoundTrip(r.Clone(requestCtx))
			cleanup := func() { stop(); cancel() }
			if err != nil || response == nil || response.Body == nil {
				cleanup()
			} else {
				response.Body = &contextBody{ReadCloser: response.Body, cleanup: cleanup}
			}
			return response, err
		})
	})
	return copy
}

// ConnectionEpoch changes when a request or stream detects a broken connection.
// UI caches can then discard data even if a watch reconnects successfully.
func (c *Client) ConnectionEpoch() uint64 {
	if c.connectionEpoch == nil {
		return 0
	}
	return c.connectionEpoch.Load()
}

type observedBody struct {
	io.ReadCloser
	ctx   context.Context
	epoch *atomic.Uint64
}

func (b *observedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && err != io.EOF && b.ctx.Err() != context.Canceled {
		b.epoch.Add(1)
	}
	return n, err
}
func observeConnection(cfg *rest.Config, epoch *atomic.Uint64) {
	cfg.Wrap(func(next http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(r *http.Request) (*http.Response, error) {
			response, err := next.RoundTrip(r)
			if r.Context().Err() != context.Canceled && (err != nil || response != nil && response.StatusCode >= 500) {
				epoch.Add(1)
			}
			if response != nil && response.Body != nil {
				response.Body = &observedBody{response.Body, r.Context(), epoch}
			}
			return response, err
		})
	})
}

func (c *Client) contextualMapper(ctx context.Context) (meta.RESTMapper, error) {
	disc, err := discovery.NewDiscoveryClientForConfig(contextualConfig(c.config, ctx))
	if err != nil {
		return nil, err
	}
	return restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(disc)), nil
}

// Keep the operation cancellation linked until the response body is closed,
// while preserving the HTTP client's timeout and original request context.
type contextBody struct {
	io.ReadCloser
	cleanup func()
}

func (b *contextBody) Close() error {
	defer b.cleanup()
	return b.ReadCloser.Close()
}

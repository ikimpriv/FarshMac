package kube

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"

	"k8s.io/client-go/rest"
)

// client-go's WebSocket and SPDY constructors do not honor rest.Config.Dial.
// Bridge only this exec's lifetime to that dialer, preserving the original TLS
// server name. Closing/cancelling exec closes the listener and every connection.
func streamDialBridge(ctx context.Context, cfg *rest.Config, endpoint *url.URL) (*rest.Config, *url.URL, func(), error) {
	if cfg.Dial == nil {
		return cfg, endpoint, func() {}, nil
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, nil, err
	}
	target := endpoint.Host
	if endpoint.Port() == "" {
		port := "443"
		if endpoint.Scheme == "http" {
			port = "80"
		}
		target = net.JoinHostPort(endpoint.Hostname(), port)
	}
	life, cancel := context.WithCancel(ctx)
	var once sync.Once
	closeBridge := func() { once.Do(func() { cancel(); listener.Close() }) }
	stop := context.AfterFunc(life, func() { listener.Close() })
	go func() {
		for {
			local, e := listener.Accept()
			if e != nil {
				return
			}
			go func() {
				defer local.Close()
				stopLocal := context.AfterFunc(life, func() { local.Close() })
				defer stopLocal()
				remote, e := cfg.Dial(life, "tcp", target)
				if e != nil {
					return
				}
				defer remote.Close()
				stopRemote := context.AfterFunc(life, func() { remote.Close() })
				defer stopRemote()
				go func() { _, _ = io.Copy(remote, local); remote.Close(); local.Close() }()
				_, _ = io.Copy(local, remote)
			}()
		}
	}()
	copy := rest.CopyConfig(cfg)
	copy.Dial = nil
	copy.Proxy = func(*http.Request) (*url.URL, error) { return nil, nil }
	if copy.ServerName == "" {
		copy.ServerName = endpoint.Hostname()
	}
	u := *endpoint
	u.Host = listener.Addr().String()
	return copy, &u, func() { closeBridge(); stop() }, nil
}

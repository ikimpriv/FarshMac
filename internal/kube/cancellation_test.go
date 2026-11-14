package kube

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"farshmac/internal/kube/kubetest"
	"k8s.io/client-go/rest"
)

func TestDiscoveryCancellationDoesNotBlockCacheReaders(t *testing.T) {
	f := kubetest.New(t)
	started := make(chan struct{})
	var once sync.Once
	cfg := &rest.Config{Host: f.URL(), WrapTransport: func(next http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/version" {
				once.Do(func() { close(started) })
				<-r.Context().Done()
				return nil, r.Context().Err()
			}
			return next.RoundTrip(r)
		})
	}}
	c, err := NewFromConfig(context.Background(), cfg, Profile{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.Resources(ctx); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("discovery not started")
	}
	read := make(chan struct{})
	go func() { c.ResourceByFolder("pods"); close(read) }()
	select {
	case <-read:
	case <-time.After(time.Second):
		t.Error("UI cache lookup blocked behind discovery")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("discovery did not cancel")
	}
}

func TestConnectCancellation(t *testing.T) {
	started := make(chan struct{})
	cfg := &rest.Config{Host: "https://cluster.invalid", Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := NewFromConfig(ctx, cfg, Profile{}); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("connect not started")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("connect did not cancel")
	}
}

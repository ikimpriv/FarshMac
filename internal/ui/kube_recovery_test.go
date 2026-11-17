package ui

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"farshmac/internal/kube"
	"farshmac/internal/kube/kubetest"
	"k8s.io/client-go/rest"
)

type kubeTestTransport func(*http.Request) (*http.Response, error)

func (f kubeTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestKubeManifestCancelAndDisconnect(t *testing.T) {
	for _, action := range []string{"escape", "cancel button", "disconnect"} {
		t.Run(action, func(t *testing.T) {
			cancelRequest := action != "disconnect"
			a, f := kubeApp(t)
			p := &a.Panels[0]
			old := p.Kube
			old.close()
			var blocked atomic.Bool
			started := make(chan struct{})
			var once sync.Once
			cfg := &rest.Config{Host: f.URL(), WrapTransport: func(next http.RoundTripper) http.RoundTripper {
				return kubeTestTransport(func(r *http.Request) (*http.Response, error) {
					if blocked.Load() && r.URL.Query().Get("watch") != "true" {
						once.Do(func() { close(started) })
						if cancelRequest {
							<-r.Context().Done()
							return nil, r.Context().Err()
						}
						return nil, errors.New("connection lost")
					}
					return next.RoundTrip(r)
				})
			}}
			client, err := kube.NewFromConfig(context.Background(), cfg, kube.Profile{Context: "fake"})
			if err != nil {
				t.Fatal(err)
			}
			p.Kube = newKubePanel(client.Profile, client, nil, a.Settings)
			if err := p.Load("/"); err != nil {
				t.Fatal(err)
			}
			waitNetwork(t, a)
			enterKube(t, a, "shop")
			enterKube(t, a, "deployments")
			selectEntry(t, p, "app")
			blocked.Store(true)
			a.key(Message{Key: "F3"})
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("request did not start")
			}
			if action == "escape" {
				a.key(Message{Key: "Escape"})
			} else if action == "cancel button" {
				r := a.progressBounds()
				pos := buttonLayout([]string{"Cancel"}, r.W)[0]
				a.click(Message{X: r.X + pos.X, Y: r.Y + r.H - 4 + pos.Y})
			}
			deadline := time.Now().Add(time.Second)
			for a.Busy && time.Now().Before(deadline) {
				a.Poll()
				time.Sleep(time.Millisecond)
			}
			if a.Busy || a.Doc != nil {
				t.Fatal("request did not stop promptly", a.Status)
			}
			if !cancelRequest {
				p.Kube.syncConnection()
				p.Kube.mu.Lock()
				cached := len(p.Kube.snapshots) + len(p.Kube.pods) + len(p.Kube.probes) + len(p.Kube.watches) + len(p.Kube.namespaces)
				p.Kube.mu.Unlock()
				if cached != 0 {
					t.Fatal("disconnected cluster retained caches", cached)
				}
			}
		})
	}
}

func TestVolumeLeavesKubeAndRestoresFileView(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.setView(0, "Brief")
	f := kubetest.New(t)
	a.connectKube(0, kube.Profile{Kubeconfig: f.Kubeconfig(t), Context: "fake"})
	waitNetwork(t, a)
	k := a.Panels[0].Kube
	a.setView(0, "Quick view")
	a.Panels[0].Hidden = true
	target := t.TempDir()
	a.openVolume(0, target)
	waitNetwork(t, a)
	p := &a.Panels[0]
	if p.Kube != nil || p.Hidden || p.View != "Brief" || p.Path != target || a.Active != 0 {
		t.Fatalf("unexpected panel: view=%s path=%s hidden=%v kube=%v", p.View, p.Path, p.Hidden, p.Kube != nil)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.watches) != 0 {
		t.Fatal("abandoned watches still running")
	}
}

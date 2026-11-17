package kube

import (
	"context"
	"fmt"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStorageMetricsWithoutMetricsServer(t *testing.T) {
	var summaries atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/namespaces/ns/pods/pod":
			fmt.Fprint(w, `{"metadata":{"name":"pod","namespace":"ns","uid":"uid"},"spec":{"nodeName":"node","containers":[{"name":"app"}]}}`)
		case "/api/v1/nodes/node/proxy/stats/summary":
			summaries.Add(1)
			fmt.Fprintf(w, `{"pods":[{"podRef":{"name":"pod","namespace":"ns","uid":"uid"},"containers":[{"name":"app","rootfs":{"time":%q,"usedBytes":40},"logs":{"time":%q,"usedBytes":2}}]}]}`, time.Now().Format(time.RFC3339Nano), time.Now().Format(time.RFC3339Nano))
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	core, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{core: core}
	ref := ContainerRef{Namespace: "ns", Pod: "pod"}
	for i := 0; i < 2; i++ {
		body, err := c.StorageMetrics(context.Background(), ref, "uid")
		if err != nil || !strings.Contains(string(body), `container="app"} 42`) {
			t.Fatal(string(body), err)
		}
	}
	if summaries.Load() != 1 {
		t.Fatal("summary was not cached")
	}
	if _, err := c.StorageMetrics(context.Background(), ref, "different-uid"); err == nil {
		t.Fatal("replacement pod accepted")
	}
}

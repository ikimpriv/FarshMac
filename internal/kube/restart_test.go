package kube

import (
	"context"
	"encoding/json"
	"fmt"
	jsonpatch "gopkg.in/evanphx/json-patch.v4"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

func TestRestartWorkload(t *testing.T) {
	for _, tc := range []struct {
		resource, spec, uid string
		forbidden           bool
		want                string
	}{
		{"deployments", `{}`, "original", false, ""},
		{"statefulsets", `{}`, "original", false, ""},
		{"daemonsets", `{}`, "original", false, ""},
		{"deployments", `{"paused":true}`, "original", false, "paused"},
		{"statefulsets", `{"updateStrategy":{"type":"OnDelete"}}`, "original", false, "OnDelete"},
		{"daemonsets", `{}`, "replacement", false, "replaced"},
		{"deployments", `{}`, "original", true, "forbidden"},
	} {
		t.Run(tc.resource+tc.want, func(t *testing.T) {
			patches := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPatch {
					patches++
					if tc.forbidden {
						w.WriteHeader(403)
						_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","message":"forbidden","code":403}`))
						return
					}
					if r.Header.Get("Content-Type") != "application/merge-patch+json" {
						t.Error("wrong patch type")
					}
					var patch map[string]any
					if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
						t.Error(err)
					}
					metadata := patch["metadata"].(map[string]any)
					if metadata["uid"] != "original" || metadata["resourceVersion"] != "42" {
						t.Error(metadata)
					}
					b, _ := json.Marshal(patch)
					if !strings.Contains(string(b), "kubectl.kubernetes.io/restartedAt") || strings.Contains(string(b), "replicas") {
						t.Error(string(b))
					}
				}
				_, _ = w.Write([]byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"test","uid":"` + tc.uid + `","resourceVersion":"42"},"spec":` + tc.spec + `}`))
			}))
			defer server.Close()
			dyn, err := dynamic.NewForConfig(&rest.Config{Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			c := &Client{dyn: dyn}
			err = c.Restart(context.Background(), Object{GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: tc.resource}, Name: "web", Namespace: "test", UID: "original"})
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("error: %v", err)
			}
			expected := 0
			if tc.want == "" || tc.forbidden {
				expected = 1
			}
			if patches != expected {
				t.Fatalf("patches %d", patches)
			}
		})
	}
}

func TestRestartOnlySupportedControllers(t *testing.T) {
	for _, group := range []string{"", "batch", "example.com"} {
		if CanRestart(&Object{GVR: schema.GroupVersionResource{Group: group, Resource: "deployments"}}) {
			t.Fatal(group)
		}
	}
	if CanRestart(&Object{GVR: schema.GroupVersionResource{Group: "apps", Resource: "replicasets"}}) {
		t.Fatal("replicaset restart")
	}
}

// Apply the actual wire patch to a complete five-replica object, rather than
// returning a canned response that could conceal accidental scale changes.
func TestRestartPreservesScaleAndTemplate(t *testing.T) {
	original := []byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"test","uid":"original","resourceVersion":"42"},"spec":{"replicas":5,"strategy":{"type":"Recreate"},"template":{"metadata":{"annotations":{"keep":"yes"}},"spec":{"containers":[{"name":"app","image":"example:test"}]}}}}`)
	for _, mutateScale := range []bool{false, true} {
		t.Run(fmt.Sprint(mutateScale), func(t *testing.T) {
			var applied []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					_, _ = w.Write(original)
					return
				}
				patch, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				var fields map[string]any
				_ = json.Unmarshal(patch, &fields)
				if fields["spec"].(map[string]any)["replicas"] != float64(5) {
					t.Error("replicas must be preserved explicitly", string(patch))
				}
				applied, err = jsonpatch.MergePatch(original, patch)
				if err != nil {
					t.Error(err)
				}
				if mutateScale {
					applied, err = jsonpatch.MergePatch(applied, []byte(`{"spec":{"replicas":1}}`))
					if err != nil {
						t.Error(err)
					}
				}
				_, _ = w.Write(applied)
			}))
			defer server.Close()
			dyn, err := dynamic.NewForConfig(&rest.Config{Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			err = (&Client{dyn: dyn}).Restart(context.Background(), Object{GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, Name: "web", Namespace: "test", UID: "original"})
			if mutateScale {
				if err == nil || !strings.Contains(err.Error(), "replicas") {
					t.Fatal(err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var before, after map[string]any
			_ = json.Unmarshal(original, &before)
			_ = json.Unmarshal(applied, &after)
			annotations := after["spec"].(map[string]any)["template"].(map[string]any)["metadata"].(map[string]any)["annotations"].(map[string]any)
			if annotations["kubectl.kubernetes.io/restartedAt"] == nil {
				t.Fatal("missing restart")
			}
			delete(annotations, "kubectl.kubernetes.io/restartedAt")
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("restart changed unrelated fields: %s", applied)
			}
		})
	}
}

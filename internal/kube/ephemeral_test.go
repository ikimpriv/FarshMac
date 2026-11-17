package kube

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"
)

func TestEphemeralOptionsReachAPI(t *testing.T) {
	for _, profile := range []string{"Default", "Restricted", "Network", "Processes", "Privileged"} {
		t.Run(profile, func(t *testing.T) {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "test", UID: "uid", ResourceVersion: "1"}}
			core := fake.NewSimpleClientset(pod)
			c := &Client{core: core}
			called := false
			core.PrependReactor("update", "pods", func(action kt.Action) (bool, runtime.Object, error) {
				called = true
				if action.GetSubresource() != "ephemeralcontainers" {
					t.Fatal("wrong subresource")
				}
				p := action.(kt.UpdateAction).GetObject().(*corev1.Pod)
				e := p.Spec.EphemeralContainers[0]
				if e.Image != "example/debug:1" || e.ImagePullPolicy != corev1.PullNever || !e.Stdin || !e.TTY || len(e.Command) != 0 {
					t.Fatal("image entrypoint/stream options changed", e)
				}
				s := e.SecurityContext
				if !*s.ReadOnlyRootFilesystem {
					t.Fatal("root flag lost")
				}
				if profile == "Privileged" {
					if s.Privileged == nil || !*s.Privileged {
						t.Fatal("profile ignored")
					}
				} else if s.Privileged != nil && *s.Privileged {
					t.Fatal("unexpected privileged escalation")
				}
				if profile == "Network" && len(s.Capabilities.Add) != 2 {
					t.Fatal("network caps missing")
				}
				if profile == "Processes" && (len(s.Capabilities.Add) != 1 || s.Capabilities.Add[0] != "SYS_PTRACE") {
					t.Fatal("process caps missing")
				}
				if profile == "Restricted" && (s.RunAsNonRoot == nil || !*s.RunAsNonRoot) {
					t.Fatal("non-root profile ignored")
				}
				return true, nil, errors.New("fixture stops before launch")
			})
			_, err := c.StartEphemeral(context.Background(), ContainerRef{Namespace: "test", Pod: "app"}, EphemeralOptions{Image: "example/debug:1", Profile: profile, TTY: true, ReadOnlyRoot: true, PullPolicy: "Never", TimeoutSeconds: 10})
			if !called || err == nil {
				t.Fatal(called, err)
			}
		})
	}
	zero := int64(0)
	o := EphemeralOptions{Image: "example/debug:1", Profile: "Restricted", RunAsUser: &zero, PullPolicy: "Always", TimeoutSeconds: 10}
	if o.Validate() == nil {
		t.Fatal("contradictory restricted/root settings accepted")
	}
}

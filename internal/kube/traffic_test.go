package kube

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestTrafficRequiresNetworkProfile(t *testing.T) {
	ec := corev1.EphemeralContainer{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Command: []string{"/podtoast", "idle"}, SecurityContext: fullManagementSecurityContext()}}
	if trafficCapable(ec) {
		t.Fatal("filesystem capabilities silently accepted for traffic")
	}
	ec.SecurityContext.Capabilities.Add = append(ec.SecurityContext.Capabilities.Add, "NET_ADMIN")
	if !trafficCapable(ec) {
		t.Fatal("network profile not recognized")
	}
	ec.Command = []string{"sh"}
	if trafficCapable(ec) {
		t.Fatal("arbitrary debugger treated as PodToast")
	}
}
func TestTrafficRejectsHostNamespaces(t *testing.T) {
	for _, network := range []bool{true, false} {
		p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ns"}, Spec: corev1.PodSpec{HostNetwork: network, HostPID: !network}}
		c := &Client{core: fake.NewSimpleClientset(p)}
		if _, err := c.OpenTraffic(context.Background(), ContainerRef{Namespace: "ns", Pod: "pod"}, "image", true); err == nil {
			t.Fatal("host namespace accepted")
		}
		for _, a := range c.core.(*fake.Clientset).Actions() {
			if a.GetVerb() != "get" {
				t.Fatal("host namespace request mutated cluster")
			}
		}
	}
}

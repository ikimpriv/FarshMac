package ui

import (
	"farshmac/internal/kube"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"strings"
	"testing"
)

func TestWorkloadRestartMenuSelection(t *testing.T) {
	a, _ := kubeApp(t)
	p := &a.Panels[0]
	p.Entries = nil
	for _, name := range []string{"one", "two", "other"} {
		resource := "deployments"
		if name == "other" {
			resource = "jobs"
		}
		p.Entries = append(p.Entries, Entry{Name: name, Info: &kubeEntry{object: &kube.Object{Name: name, Namespace: "shop", GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: resource}}}})
	}
	p.Marked = map[string]bool{"one": true, "two": true, "other": true}
	items := a.kubeRestartItems(p)
	if len(items) != 1 || !strings.Contains(items[0].Label, "2/3") {
		t.Fatal(items)
	}
	items[0].Run()
	if a.Form == nil || !strings.Contains(a.Form.Message, "one") || !strings.Contains(a.Form.Message, "two") || strings.Contains(a.Form.Message, "other") {
		t.Fatal(a.Form)
	}
	a.formKey(Message{Key: "Escape"})
	if a.task != nil {
		t.Fatal("cancel started restart")
	}
	p.Marked = map[string]bool{"other": true}
	if len(a.kubeRestartItems(p)) != 0 {
		t.Fatal("job offers restart")
	}
}

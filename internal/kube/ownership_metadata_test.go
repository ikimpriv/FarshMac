package kube

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestMetadataRetainsControllerOwner(t *testing.T) {
	yes := true
	o := &Object{}
	applyMetadata(o, metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{{UID: "secondary"}, {UID: "primary", Controller: &yes}}})
	if o.ControllerOwner != "primary" || len(o.Owners) != 2 {
		t.Fatal("controller owner lost", o)
	}
	applyMetadata(o, metav1.ObjectMeta{})
	if o.ControllerOwner != "" || len(o.Owners) != 0 {
		t.Fatal("stale ownership after update")
	}
}

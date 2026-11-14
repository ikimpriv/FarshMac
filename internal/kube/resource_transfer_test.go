package kube

import (
	"context"
	"farshmac/internal/kube/kubetest"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"testing"
)

func TestResourceTransferAcrossClustersAndLocalRename(t *testing.T) {
	sourceAPI, targetAPI := kubetest.New(t), kubetest.New(t)
	// Widgets are a CRD in the fake API's discovery, not a built-in resource.
	ref := Ref{GVR: schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}, Namespace: "shop", Name: "blue"}
	object := kubetest.Obj("Widget", "shop", "blue", "widget-blue", nil, nil, nil)
	object["apiVersion"] = "example.com/v1"
	object["spec"] = map[string]any{"color": "blue"}
	sourceAPI.Add("/apis/example.com/v1/namespaces/shop/widgets", object)
	source, target := testClient(t, sourceAPI), testClient(t, targetAPI)
	ctx := context.Background()
	if err := TransferResource(ctx, source, target, ref, ResourceTransferOptions{Namespace: "default"}); err != nil {
		t.Fatal(err)
	}
	copied := ref
	copied.Namespace = "default"
	got, err := target.Get(ctx, copied)
	if err != nil {
		t.Fatal(err)
	}
	if got.GetUID() == "widget-blue" || got.Object["status"] != nil {
		t.Fatal("server-managed state copied", got)
	}
	if _, err = source.Get(ctx, ref); err != nil {
		t.Fatal("copy removed source", err)
	}
	if err = TransferResource(ctx, source, source, ref, ResourceTransferOptions{Namespace: "shop", Name: "renamed", Move: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = source.Get(ctx, ref); !apierrors.IsNotFound(err) {
		t.Fatal("source remains", err)
	}
	renamed := ref
	renamed.Name = "renamed"
	if _, err = source.Get(ctx, renamed); err != nil {
		t.Fatal(err)
	}
	// A failed destination write must never delete the source.
	if err = TransferResource(ctx, source, target, renamed, ResourceTransferOptions{Namespace: "default", Name: "blue", Move: true}); !apierrors.IsAlreadyExists(err) {
		t.Fatal("expected collision", err)
	}
	if _, err = source.Get(ctx, renamed); err != nil {
		t.Fatal("collision removed source", err)
	}
	if err = TransferResource(ctx, source, source, renamed, ResourceTransferOptions{Namespace: "shop", Move: true}); err == nil {
		t.Fatal("self move allowed")
	}
}

type afterWriteStore struct {
	ResourceStore
	after func()
}

func (s afterWriteStore) WriteTransfer(ctx context.Context, ref Ref, obj *unstructured.Unstructured, replace, force bool) error {
	if err := s.ResourceStore.WriteTransfer(ctx, ref, obj, replace, force); err != nil {
		return err
	}
	s.after()
	return nil
}

func TestResourceMoveKeepsChangedSource(t *testing.T) {
	f := kubetest.New(t)
	kubetest.SeedShop(f)
	c := testClient(t, f)
	ctx := context.Background()
	ref := Ref{GVR: gvrDeploy, Namespace: "shop", Name: "app"}
	target := afterWriteStore{ResourceStore: c, after: func() {
		obj, err := c.Get(ctx, ref)
		if err != nil {
			t.Fatal(err)
		}
		obj.SetAnnotations(map[string]string{"concurrent-update": "true"})
		if _, err = c.Update(ctx, obj); err != nil {
			t.Fatal(err)
		}
	}}
	err := TransferResource(ctx, c, target, ref, ResourceTransferOptions{Namespace: "default", Move: true})
	if !apierrors.IsConflict(err) {
		t.Fatal("expected guarded deletion to fail", err)
	}
	if _, err = c.Get(ctx, ref); err != nil {
		t.Fatal("changed source deleted", err)
	}
	ref.Namespace = "default"
	if _, err = c.Get(ctx, ref); err != nil {
		t.Fatal("destination should remain", err)
	}
}

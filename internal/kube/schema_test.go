package kube

import (
	"context"
	"errors"
	"strings"
	"testing"

	"farshmac/internal/kube/kubetest"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var widgetGVK = schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"}

func widgetSchema(t *testing.T) (*Client, *Schema) {
	t.Helper()
	f := kubetest.New(t)
	c := testClient(t, f)
	s, err := c.SchemaFor(context.Background(), widgetGVK)
	if err != nil {
		t.Fatal(err)
	}
	return c, s
}

func TestSchemaForResolvesAndCaches(t *testing.T) {
	c, s := widgetSchema(t)
	if s.Type != "object" || s.Properties["spec"] == nil || s.Properties["metadata"] == nil {
		t.Fatalf("widget schema %+v", s)
	}
	meta := s.Properties["metadata"]
	if meta.Properties["name"] == nil || meta.Properties["labels"].Additional == nil || meta.Properties["labels"].Additional.Type != "string" {
		t.Fatalf("ObjectMeta via allOf $ref: %+v", meta)
	}
	spec := s.Properties["spec"]
	if spec.Required[0] != "size" || spec.Properties["mode"].Enum[1] != "slow" || !spec.Properties["replicas"].IntOrString || !spec.Properties["options"].PreserveUnknown {
		t.Fatalf("spec %+v", spec)
	}
	if spec.Properties["items"].Type != "array" || spec.Properties["items"].Items.Properties["name"].Type != "string" {
		t.Fatalf("items %+v", spec.Properties["items"])
	}
	again, _ := c.SchemaFor(context.Background(), widgetGVK)
	if again != s {
		t.Fatal("schema not cached")
	}
	dep, err := c.SchemaFor(context.Background(), schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"})
	if err != nil || dep.Properties["spec"].Properties["template"].Properties["spec"].Properties["containers"].Items.Properties["image"].Type != "string" {
		t.Fatal("deployment $ref chain", err)
	}
	if dep.Properties["spec"].Properties["selector"].Properties["matchLabels"].Additional.Type != "string" {
		t.Fatal("map-like object")
	}
	if _, err := c.SchemaFor(context.Background(), schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Gadget"}); !errors.Is(err, ErrNoSchema) {
		t.Fatal("unknown kind", err)
	}
	if _, err := c.SchemaFor(context.Background(), schema.GroupVersionKind{Group: "nothing.io", Version: "v9", Kind: "X"}); !errors.Is(err, ErrNoSchema) {
		t.Fatal("unknown group", err)
	}
	if s.Properties["spec"].Summary() != "Desired state of the widget." {
		t.Fatal("summary", s.Properties["spec"].Summary())
	}
}

const validWidget = `apiVersion: example.com/v1
kind: Widget
metadata:
  name: w1
  labels:
    app: shop
spec:
  size: 3
  mode: fast
  replicas: 50%
  options:
    anything: goes
    nested:
      deeper: true
  items:
    - name: a
      weight: 1.5
    - name: b
  enabled: true
status:
  phase: Ready
`

func problems(t *testing.T, s *Schema, doc string) []Problem {
	t.Helper()
	return CheckManifest([]byte(doc), s)
}

func TestCheckManifest(t *testing.T) {
	_, s := widgetSchema(t)
	if p := problems(t, s, validWidget); len(p) != 0 {
		t.Fatalf("valid manifest: %+v", p)
	}
	if p := problems(t, s, strings.Replace(validWidget, "  mode: fast", "  mode: fast\n  colour: red", 1)); len(p) != 1 || p[0].Line != 10 || p[0].Path != "spec.colour" || !strings.Contains(p[0].Message, "unknown field") {
		t.Fatalf("unknown field: %+v", p)
	}
	if p := problems(t, s, strings.Replace(validWidget, "  size: 3", "  size: three", 1)); len(p) != 1 || p[0].Line != 8 || p[0].Path != "spec.size" || !strings.Contains(p[0].Message, "integer") {
		t.Fatalf("wrong type: %+v", p)
	}
	if p := problems(t, s, strings.Replace(validWidget, "mode: fast", "mode: medium", 1)); len(p) != 1 || !strings.Contains(p[0].Message, "fast, slow") {
		t.Fatalf("enum: %+v", p)
	}
	if p := problems(t, s, strings.Replace(validWidget, "replicas: 50%", "replicas: 5", 1)); len(p) != 0 {
		t.Fatalf("int-or-string: %+v", p)
	}
	if p := problems(t, s, strings.Replace(validWidget, "  size: 3\n", "", 1)); len(p) != 1 || p[0].Line != 7 || p[0].Path != "spec" || !strings.Contains(p[0].Message, "size") {
		t.Fatalf("required: %+v", p)
	}
	if p := problems(t, s, strings.Replace(validWidget, "    - name: b", "    - weight: 2", 1)); len(p) != 1 || p[0].Path != "spec.items[1]" || !strings.Contains(p[0].Message, "name") {
		t.Fatalf("nested required: %+v", p)
	}
	if p := problems(t, s, strings.Replace(validWidget, "  enabled: true", "  enabled: yes please", 1)); len(p) != 1 || !strings.Contains(p[0].Message, "true or false") {
		t.Fatalf("boolean: %+v", p)
	}
	if p := problems(t, s, "spec:\n  size: 1\n  items: notalist\n"); len(p) != 1 || !strings.Contains(p[0].Message, "list") {
		t.Fatalf("array: %+v", p)
	}
	if p := problems(t, s, "apiVersion: v1\nkind: Widget\nspec:\n  size: [1\n"); len(p) != 1 || p[0].Line < 3 || p[0].Line > 4 {
		t.Fatalf("parse error: %+v", p)
	}
	anchors := "apiVersion: example.com/v1\nkind: Widget\nmetadata: &m\n  name: a\nspec:\n  size: 1\n  options: *m\n"
	if p := problems(t, s, anchors); len(p) != 0 {
		t.Fatalf("anchors: %+v", p)
	}
	if p := problems(t, s, "spec:\n  size: 1\n  mode: |\n    fast\n"); len(p) != 1 || !strings.Contains(p[0].Message, "fast, slow") {
		t.Fatalf("multi-line string reaches the enum check: %+v", p)
	}
	if p := CheckManifest([]byte(validWidget), nil); p != nil {
		t.Fatal("nil schema")
	}
}

func labels(items []Completion) string {
	var out []string
	for _, c := range items {
		out = append(out, c.Label)
	}
	return strings.Join(out, ",")
}

func TestCompleteManifest(t *testing.T) {
	_, s := widgetSchema(t)
	doc := "apiVersion: example.com/v1\nkind: Widget\nmetadata:\n  name: w1\nspec:\n  size: 3\n  re\n  items:\n    - name: a\n      \n  mode: \n"
	// Keys under spec with a prefix: replicas; size is already present.
	prefix, items := CompleteManifest([]byte(doc), 7, 4, s)
	if prefix != "re" || labels(items) != "replicas" || items[0].Insert != "plicas: " || !strings.Contains(items[0].Detail, "int or string") || items[0].Type != "int or string" {
		t.Fatalf("prefix completion: %q %+v", prefix, items)
	}
	// Without a prefix: every spec key not yet present, sorted; objects insert "key:".
	prefix, items = CompleteManifest([]byte("spec:\n  size: 1\n  \n"), 3, 2, s)
	if prefix != "" || labels(items) != "enabled,items,mode,options,replicas" {
		t.Fatalf("spec keys: %q %s", prefix, labels(items))
	}
	for _, c := range items {
		switch c.Label {
		case "items":
			if c.Insert != "items:\n    - " || c.Type != "[]object" {
				t.Fatalf("array insert %+v", c)
			}
		case "options":
			if c.Insert != "options:\n    " || c.Type != "object" {
				t.Fatalf("object insert %+v", c)
			}
		case "mode":
			if c.Insert != "mode: " || c.Type != "string (enum: fast, slow)" {
				t.Fatalf("scalar insert %+v", c)
			}
		}
	}
	// Required fields come first and are marked; indentation follows the key.
	_, items = CompleteManifest([]byte("spec:\n  items:\n    - "), 3, 6, s)
	if labels(items) != "name *,weight" || !items[0].Required || items[0].Insert != "name: " {
		t.Fatalf("required first: %+v", items)
	}
	_, items = CompleteManifest([]byte("apiVersion: v1\nkind: Widget\n"), 3, 0, s)
	for _, c := range items {
		if c.Label == "spec" && c.Insert != "spec:\n  " || c.Label == "metadata" && c.Insert != "metadata:\n  " {
			t.Fatalf("root object insert %+v", c)
		}
	}
	// Value completion: enum and booleans.
	prefix, items = CompleteManifest([]byte(doc), 11, 8, s)
	if prefix != "" || labels(items) != "fast,slow" {
		t.Fatalf("enum values: %q %s", prefix, labels(items))
	}
	prefix, items = CompleteManifest([]byte("spec:\n  enabled: t"), 2, 12, s)
	if prefix != "t" || labels(items) != "true" || items[0].Insert != "rue" {
		t.Fatalf("boolean value: %q %+v", prefix, items)
	}
	// A key under "- " in an array of objects, and a continuation line of the item.
	prefix, items = CompleteManifest([]byte("spec:\n  items:\n    - n"), 3, 7, s)
	if prefix != "n" || labels(items) != "name *" {
		t.Fatalf("item key: %q %s", prefix, labels(items))
	}
	prefix, items = CompleteManifest([]byte(doc), 10, 6, s)
	if prefix != "" || labels(items) != "weight" {
		t.Fatalf("item continuation (name present): %q %s", prefix, labels(items))
	}
	// Root level.
	_, items = CompleteManifest([]byte("apiVersion: v1\nkind: Widget\ns"), 3, 1, s)
	if labels(items) != "spec,status" {
		t.Fatalf("root keys: %s", labels(items))
	}
	// Nested object keys and a map-like object (no fixed keys).
	_, items = CompleteManifest([]byte("metadata:\n  na"), 2, 4, s)
	if labels(items) != "name,namespace" {
		t.Fatalf("metadata keys: %s", labels(items))
	}
	_, items = CompleteManifest([]byte("metadata:\n  labels:\n    a"), 3, 5, s)
	if len(items) != 0 {
		t.Fatalf("map-like object has no keys: %s", labels(items))
	}
	// Unknown parent: nothing.
	if _, items = CompleteManifest([]byte("bogus:\n  x"), 2, 3, s); len(items) != 0 {
		t.Fatal("unknown parent", labels(items))
	}
	if _, items = CompleteManifest([]byte(doc), 99, 0, s); len(items) != 0 {
		t.Fatal("line out of range")
	}
}

func TestContextAt(t *testing.T) {
	_, s := widgetSchema(t)
	doc := "apiVersion: example.com/v1\nkind: Widget\nspec:\n  size: 3\n  mode: fast\n  items:\n    - name: a\n      weight: 1\n  \n"
	c := ContextAt([]byte(doc), 4, 3, s)
	if c == nil || c.Path != "spec.size" || c.Name != "size" || c.Type != "integer" || !c.Required || c.InValue || c.Description != "Size in units." {
		t.Fatalf("scalar key: %+v", c)
	}
	c = ContextAt([]byte(doc), 5, 9, s)
	if c == nil || c.Name != "mode" || !c.InValue || strings.Join(c.Enum, ",") != "fast,slow" || c.Type != "string (enum: fast, slow)" {
		t.Fatalf("value position: %+v", c)
	}
	c = ContextAt([]byte(doc), 6, 4, s)
	if c == nil || c.Path != "spec.items" || c.Type != "[]object" || strings.Join(c.Children, ",") != "name *,weight" {
		t.Fatalf("array key: %+v", c)
	}
	c = ContextAt([]byte(doc), 8, 8, s)
	if c == nil || c.Path != "spec.items.weight" || c.Name != "weight" || c.Type != "number" || c.Required {
		t.Fatalf("inside an item: %+v", c)
	}
	c = ContextAt([]byte(doc), 7, 8, s)
	if c == nil || c.Name != "name" || !c.Required || c.Path != "spec.items.name" {
		t.Fatalf("item first key: %+v", c)
	}
	c = ContextAt([]byte(doc), 9, 2, s)
	if c == nil || c.Name != "" || c.Path != "spec" || c.Type != "object" || !strings.Contains(strings.Join(c.Children, ","), "size *") {
		t.Fatalf("blank line in spec: %+v", c)
	}
	c = ContextAt([]byte(doc), 3, 0, s)
	if c == nil || c.Path != "spec" || c.Name != "spec" || c.Children[0] != "size *" || c.Description != "Desired state of the widget." {
		t.Fatalf("root key: %+v", c)
	}
	if ContextAt([]byte("bogus:\n  x: 1\n"), 2, 2, s) != nil || ContextAt([]byte(doc), 0, 0, s) != nil {
		t.Fatal("unresolvable")
	}
}

func TestContextInsideSequenceItems(t *testing.T) {
	f := kubetest.New(t)
	c := testClient(t, f)
	s, err := c.SchemaFor(context.Background(), schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"})
	if err != nil {
		t.Fatal(err)
	}
	doc := "apiVersion: example.com/v1\nkind: Widget\nspec:\n  size: 1\n  items:\n    - \n    - name: a\n      \n    - name: b\n      weight: 1\n"
	// The key of an item line and the value after it resolve to the item's field,
	// even when earlier "- " lines precede it.
	if fc := ContextAt([]byte(doc), 7, 6, s); fc == nil || fc.Name != "name" || fc.InValue {
		t.Fatalf("item key context %+v", fc)
	}
	if fc := ContextAt([]byte(doc), 7, 12, s); fc == nil || fc.Name != "name" || !fc.InValue {
		t.Fatalf("item value context %+v", fc)
	}
	// A blank line inside an item lists the item's fields and completes only the missing ones.
	if fc := ContextAt([]byte(doc), 8, 6, s); fc == nil || len(fc.Children) != 2 {
		t.Fatalf("blank line in item %+v", fc)
	}
	if _, items := CompleteManifest([]byte(doc), 8, 6, s); len(items) != 1 || items[0].Label != "weight" {
		t.Fatalf("completion inside item %+v", items)
	}
	if fc := ContextAt([]byte(doc), 10, 6, s); fc == nil || fc.Name != "weight" || fc.Type != "number" {
		t.Fatalf("second key of an item %+v", fc)
	}
}

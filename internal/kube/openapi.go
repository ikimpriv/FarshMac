package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Schema is the part of a kind's OpenAPI v3 schema the editor needs to
// check a manifest and complete keys and values. CRDs publish their schemas
// into /openapi/v3 like built-in kinds, so one path serves both.
type Schema struct {
	Type            string // object, array, string, integer, number, boolean, "" (any)
	Properties      map[string]*Schema
	Required        []string
	Items           *Schema
	Enum            []string // Rendered values: strings as is, numbers and booleans formatted.
	Additional      *Schema  // additionalProperties schema; nil when unknown keys are not allowed (unless PreserveUnknown).
	AdditionalOK    bool     // additionalProperties: true, or a map-like object.
	PreserveUnknown bool     // x-kubernetes-preserve-unknown-fields
	IntOrString     bool     // x-kubernetes-int-or-string
	Nullable        bool
	Description     string
}

// ErrNoSchema is returned when the server publishes no schema for a kind.
var ErrNoSchema = errors.New("no schema published for this kind")

type openAPICache struct {
	mu        sync.Mutex
	index     map[string]string // "apis/apps/v1" -> server relative URL
	documents map[string]map[string]json.RawMessage
	schemas   map[schema.GroupVersionKind]*Schema
}

// SchemaFor fetches and resolves the OpenAPI v3 schema of a kind, cached
// for the client's lifetime.
func (c *Client) SchemaFor(ctx context.Context, gvk schema.GroupVersionKind) (*Schema, error) {
	c.openapiMu.Lock()
	if c.openapi == nil {
		c.openapi = &openAPICache{documents: map[string]map[string]json.RawMessage{}, schemas: map[schema.GroupVersionKind]*Schema{}}
	}
	cache := c.openapi
	c.openapiMu.Unlock()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if s, ok := cache.schemas[gvk]; ok {
		if s == nil {
			return nil, ErrNoSchema
		}
		return s, nil
	}
	if cache.index == nil {
		raw, err := c.raw.Get().AbsPath("/openapi/v3").Do(ctx).Raw()
		if err != nil {
			return nil, fmt.Errorf("openapi index: %w", err)
		}
		var index struct {
			Paths map[string]struct {
				ServerRelativeURL string `json:"serverRelativeURL"`
			} `json:"paths"`
		}
		if err := json.Unmarshal(raw, &index); err != nil {
			return nil, fmt.Errorf("openapi index: %w", err)
		}
		cache.index = map[string]string{}
		for p, entry := range index.Paths {
			cache.index[p] = entry.ServerRelativeURL
		}
	}
	key := "api/" + gvk.Version
	if gvk.Group != "" {
		key = "apis/" + gvk.Group + "/" + gvk.Version
	}
	components, ok := cache.documents[key]
	if !ok {
		url, found := cache.index[key]
		if !found {
			cache.schemas[gvk] = nil
			return nil, ErrNoSchema
		}
		path, query, _ := strings.Cut(url, "?")
		req := c.raw.Get().AbsPath(path)
		for _, kv := range strings.Split(query, "&") {
			if k, v, ok := strings.Cut(kv, "="); ok {
				req = req.Param(k, v)
			}
		}
		raw, err := req.Do(ctx).Raw()
		if err != nil {
			return nil, fmt.Errorf("openapi %s: %w", key, err)
		}
		var doc struct {
			Components struct {
				Schemas map[string]json.RawMessage `json:"schemas"`
			} `json:"components"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("openapi %s: %w", key, err)
		}
		components = doc.Components.Schemas
		if components == nil {
			components = map[string]json.RawMessage{}
		}
		cache.documents[key] = components
	}
	r := &schemaResolver{components: components, resolved: map[string]*Schema{}}
	for name, raw := range components {
		var probe map[string]json.RawMessage
		if json.Unmarshal(raw, &probe) != nil {
			continue
		}
		if g, ok := probe["x-kubernetes-group-version-kind"]; ok {
			var gvks []map[string]string
			if json.Unmarshal(g, &gvks) == nil {
				for _, item := range gvks {
					if item["group"] == gvk.Group && item["version"] == gvk.Version && item["kind"] == gvk.Kind {
						s := r.resolve("#/components/schemas/" + name)
						if s == nil {
							break
						}
						cache.schemas[gvk] = s
						return s, nil
					}
				}
			}
		}
	}
	cache.schemas[gvk] = nil
	return nil, ErrNoSchema
}

// rawSchema is the JSON shape of an OpenAPI schema node, as far as needed.
type rawSchema struct {
	Ref                  string                     `json:"$ref"`
	Type                 string                     `json:"type"`
	Properties           map[string]json.RawMessage `json:"properties"`
	Required             []string                   `json:"required"`
	Items                json.RawMessage            `json:"items"`
	Enum                 []json.RawMessage          `json:"enum"`
	AdditionalProperties json.RawMessage            `json:"additionalProperties"`
	AllOf                []json.RawMessage          `json:"allOf"`
	AnyOf                []json.RawMessage          `json:"anyOf"`
	OneOf                []json.RawMessage          `json:"oneOf"`
	PreserveUnknown      bool                       `json:"x-kubernetes-preserve-unknown-fields"`
	IntOrString          bool                       `json:"x-kubernetes-int-or-string"`
	Nullable             bool                       `json:"nullable"`
	Description          string                     `json:"description"`
}

type schemaResolver struct {
	components map[string]json.RawMessage
	resolved   map[string]*Schema // By $ref; entries are shared so cycles terminate.
}

func (r *schemaResolver) resolve(ref string) *Schema {
	if s, ok := r.resolved[ref]; ok {
		return s
	}
	name := strings.TrimPrefix(ref, "#/components/schemas/")
	raw, ok := r.components[name]
	if !ok {
		return nil
	}
	s := &Schema{}
	r.resolved[ref] = s // Placeholder first: a self-referencing schema finds it.
	r.fill(s, raw)
	return s
}

// node builds a schema for an inline node, or returns the shared schema of
// a $ref.
func (r *schemaResolver) node(raw json.RawMessage) *Schema {
	if len(raw) == 0 {
		return nil
	}
	var head struct {
		Ref string `json:"$ref"`
	}
	if json.Unmarshal(raw, &head) == nil && head.Ref != "" {
		var rest map[string]json.RawMessage
		json.Unmarshal(raw, &rest)
		if len(rest) <= 2 { // $ref plus maybe description: share the target.
			if s := r.resolve(head.Ref); s != nil {
				return s
			}
		}
	}
	s := &Schema{}
	r.fill(s, raw)
	return s
}

func (r *schemaResolver) fill(s *Schema, raw json.RawMessage) {
	var in rawSchema
	if json.Unmarshal(raw, &in) != nil {
		return
	}
	if in.Ref != "" {
		if target := r.resolve(in.Ref); target != nil {
			*s = *target
		}
	}
	// allOf/anyOf/oneOf with one entry wrap a $ref (CRDs, ObjectMeta).
	for _, group := range [][]json.RawMessage{in.AllOf, in.AnyOf, in.OneOf} {
		for _, part := range group {
			if sub := r.node(part); sub != nil {
				merge(s, sub)
			}
		}
	}
	if in.Type != "" {
		s.Type = in.Type
	}
	if in.Description != "" {
		s.Description = in.Description
	}
	if in.PreserveUnknown {
		s.PreserveUnknown = true
	}
	if in.IntOrString {
		s.IntOrString = true
	}
	if in.Nullable {
		s.Nullable = true
	}
	if len(in.Required) > 0 {
		s.Required = append(s.Required, in.Required...)
	}
	if len(in.Properties) > 0 {
		if s.Properties == nil {
			s.Properties = map[string]*Schema{}
		}
		for name, p := range in.Properties {
			if sub := r.node(p); sub != nil {
				s.Properties[name] = sub
			}
		}
		if s.Type == "" {
			s.Type = "object"
		}
	}
	if len(in.Items) > 0 {
		s.Items = r.node(in.Items)
		if s.Type == "" {
			s.Type = "array"
		}
	}
	for _, e := range in.Enum {
		var v any
		if json.Unmarshal(e, &v) == nil {
			s.Enum = append(s.Enum, renderValue(v))
		}
	}
	if len(in.AdditionalProperties) > 0 {
		var allow bool
		if json.Unmarshal(in.AdditionalProperties, &allow) == nil {
			s.AdditionalOK = allow
		} else {
			s.Additional = r.node(in.AdditionalProperties)
			s.AdditionalOK = true
			if s.Type == "" {
				s.Type = "object"
			}
		}
	}
}

func merge(dst, src *Schema) {
	if dst.Type == "" {
		dst.Type = src.Type
	}
	if dst.Description == "" {
		dst.Description = src.Description
	}
	if len(src.Properties) > 0 && dst.Properties == nil {
		dst.Properties = map[string]*Schema{}
	}
	for k, v := range src.Properties {
		if _, ok := dst.Properties[k]; !ok {
			dst.Properties[k] = v
		}
	}
	dst.Required = append(dst.Required, src.Required...)
	if dst.Items == nil {
		dst.Items = src.Items
	}
	if len(dst.Enum) == 0 {
		dst.Enum = src.Enum
	}
	if dst.Additional == nil {
		dst.Additional = src.Additional
	}
	dst.AdditionalOK = dst.AdditionalOK || src.AdditionalOK
	dst.PreserveUnknown = dst.PreserveUnknown || src.PreserveUnknown
	dst.IntOrString = dst.IntOrString || src.IntOrString
	dst.Nullable = dst.Nullable || src.Nullable
}

func renderValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%g", x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case nil:
		return "null"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// Summary is the first sentence of the description, for completion details.
func (s *Schema) Summary() string {
	d := strings.TrimSpace(s.Description)
	if i := strings.Index(d, ". "); i > 0 {
		d = d[:i+1]
	}
	if i := strings.IndexByte(d, '\n'); i > 0 {
		d = d[:i]
	}
	return d
}

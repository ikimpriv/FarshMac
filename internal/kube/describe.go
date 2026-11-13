package kube

import (
	"context"
	"fmt"

	"k8s.io/kubectl/pkg/describe"
)

// Describe renders the object the way kubectl describe does, events
// included. Built-in kinds use their describers; everything else, CRDs
// included, the generic one.
func (c *Client) Describe(ctx context.Context, ref Ref) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	mapping, err := c.mapping(ref.GVR)
	if err != nil {
		return "", err
	}
	describer, ok := describe.DescriberFor(mapping.GroupVersionKind.GroupKind(), c.config)
	if !ok {
		if describer, ok = describe.GenericDescriberFor(mapping, c.config); !ok {
			return "", fmt.Errorf("no describer for %s", mapping.GroupVersionKind.Kind)
		}
	}
	type result struct {
		text string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		defer func() {
			// Describers index into optional spec fields; an unusual object
			// must not take the UI down.
			if r := recover(); r != nil {
				done <- result{"", fmt.Errorf("describe %s: %v", ref, r)}
			}
		}()
		text, err := describer.Describe(ref.Namespace, ref.Name, describe.DescriberSettings{ShowEvents: true})
		done <- result{text, err}
	}()
	select {
	case r := <-done:
		return r.text, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

package aispaces

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type runtimeCacheEntry struct {
	done  chan struct{}
	text  string
	err   error
	until time.Time
}
type runtimeCache struct {
	mu      sync.Mutex
	entries map[string]*runtimeCacheEntry
}

var sharedRuntime runtimeCache

func runtimeKey(browser bool) string {
	key := fmt.Sprint(browser) + ":" + os.Getenv("PATH") + ":" + chromePath()
	if info, err := os.Stat(chromePath()); err == nil {
		key += fmt.Sprintf(":%d:%d", info.Size(), info.ModTime().UnixNano())
	}
	return key
}
func (c *runtimeCache) get(ctx context.Context, key string, probe func(context.Context) (string, error)) (string, error) {
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]*runtimeCacheEntry{}
	}
	if e := c.entries[key]; e != nil && (e.until.IsZero() || time.Now().Before(e.until)) {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-e.done:
		}
		var value map[string]any
		_ = json.Unmarshal([]byte(e.text), &value)
		if value != nil {
			value["cached"] = true
			return jsonText(value), e.err
		}
		return e.text, e.err
	}
	// Keys depend only on local runtime identity; evict expired completed entries.
	for k, e := range c.entries {
		if !e.until.IsZero() && time.Now().After(e.until) {
			delete(c.entries, k)
		}
	}
	e := &runtimeCacheEntry{done: make(chan struct{})}
	c.entries[key] = e
	c.mu.Unlock()
	text, err := probe(ctx)
	c.mu.Lock()
	e.text, e.err = text, err
	e.until = time.Now().Add(10 * time.Minute)
	if err != nil || strings.Contains(text, `"ready":false`) {
		e.until = time.Now().Add(15 * time.Second)
	}
	close(e.done)
	c.mu.Unlock()
	return text, err
}
func cachedRuntime() any {
	sharedRuntime.mu.Lock()
	defer sharedRuntime.mu.Unlock()
	e := sharedRuntime.entries[runtimeKey(true)]
	if e == nil || e.until.IsZero() || time.Now().After(e.until) || e.err != nil {
		return nil
	}
	var result map[string]any
	if json.Unmarshal([]byte(e.text), &result) != nil {
		return nil
	}
	result["cached"] = true
	return result
}
func runtimeCheck(ctx context.Context, browser bool) (string, error) {
	return sharedRuntime.get(ctx, runtimeKey(browser), func(ctx context.Context) (string, error) { return probeRuntime(ctx, browser) })
}

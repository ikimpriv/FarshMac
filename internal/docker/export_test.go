package docker

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type countSink struct{ bytes int }

func (c *countSink) Write(b []byte) (int, error) { c.bytes += len(b); return len(b), nil }
func TestRunToStreamsAndCancels(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "docker")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nif [ \"$1\" = slow ]; then exec sleep 30; fi\ndd if=/dev/zero bs=1048576 count=20 2>/dev/null\n"), 0700); err != nil {
		t.Fatal(err)
	}
	client := Client{Binary: binary}
	sink := &countSink{}
	if err := client.RunTo(context.Background(), sink, "image", "save", "fixture"); err != nil {
		t.Fatal(err)
	}
	if sink.bytes != 20<<20 {
		t.Fatal("export truncated", sink.bytes)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := client.RunTo(ctx, io.Discard, "slow"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("cancellation too slow")
	}
}

package terminal

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestHistoryRotationPageIndexPermissionsAndCleanup(t *testing.T) {
	h, err := newHistory(180, 650)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	for i := 0; i < 100; i++ {
		line := json.RawMessage(fmt.Sprintf(`{"c":80,"w":false,"r":[["row-%d",16711680,0,1]]}`, i))
		if _, err = h.Append([]json.RawMessage{line}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := h.Page(0, 256)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 100 || page.First == 0 || page.Start != page.First || page.Bytes > 650 {
		t.Fatal(page.HistoryStatus, page.Start)
	}
	for i, line := range page.Lines {
		if !strings.Contains(string(line), fmt.Sprintf(`"row-%d"`, page.Start+int64(i))) {
			t.Fatal("bad on-disk offset", string(line))
		}
	}
	for _, path := range []string{h.dir, h.segments[len(h.segments)-1].path + ".rows", h.segments[len(h.segments)-1].path + ".idx"} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0077 != 0 {
			t.Fatal("history is not private", path, info.Mode())
		}
	}
	dir := h.dir
	if err = h.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("temporary history survived Close", err)
	}
	if _, err = h.Page(0, 10); err == nil {
		t.Fatal("read closed history")
	}
}

func TestHistoryLargeDoesNotKeepRowsOrIndexInMemory(t *testing.T) {
	h, err := NewHistory()
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	line := json.RawMessage(`{"c":80,"w":false,"r":[["large log line",12632256,0,0]]}`)
	batch := make([]json.RawMessage, 1000)
	for i := range batch {
		batch[i] = line
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < 200; i++ {
		if _, err = h.Append(batch); err != nil {
			t.Fatal(err)
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	if after.HeapAlloc > before.HeapAlloc+4<<20 {
		t.Fatal("history heap grew with line count", before.HeapAlloc, after.HeapAlloc)
	}
	page, err := h.Page(199900, 100)
	if err != nil || len(page.Lines) != 100 || page.Total != 200000 {
		t.Fatal("large page", len(page.Lines), err)
	}
	if len(h.segments) > 2 {
		t.Fatal("unexpected segment metadata growth", len(h.segments))
	}
}

func TestHistoryReportsArchiveFailure(t *testing.T) {
	h, err := NewHistory()
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if _, err = h.Append([]json.RawMessage{json.RawMessage(`{"r":[]}`)}); err != nil {
		t.Fatal(err)
	}
	status, err := h.Append([]json.RawMessage{json.RawMessage(strings.Repeat("x", 300<<10))})
	if err == nil || status.Error == "" {
		t.Fatal("oversized history silently dropped")
	}
	page, err := h.Page(0, 10)
	if err != nil || len(page.Lines) != 1 {
		t.Fatal("failure destroyed preceding history", err)
	}
}

func BenchmarkHistoryMillionLines(b *testing.B) {
	line := json.RawMessage(`{"c":120,"w":false,"r":[["long-running command output",12632256,0,0]]}`)
	batch := make([]json.RawMessage, 1000)
	for i := range batch {
		batch[i] = line
	}
	b.SetBytes(int64(len(line)+12) * 1000000)
	for n := 0; n < b.N; n++ {
		h, err := NewHistory()
		if err != nil {
			b.Fatal(err)
		}
		for i := 0; i < 1000; i++ {
			if _, err = h.Append(batch); err != nil {
				b.Fatal(err)
			}
		}
		page, err := h.Page(999900, 100)
		if err != nil || len(page.Lines) != 100 {
			b.Fatal("last page", err)
		}
		if err = h.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

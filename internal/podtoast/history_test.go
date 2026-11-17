package podtoast

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHistoryBoundsAndEndpoint(t *testing.T) {
	h := MetricsHistory{}
	now := time.Now()
	for i := 0; i < 300; i++ {
		h.Append(now.Add(time.Duration(i)*HistoryInterval), strings.Repeat("x", 20<<10))
	}
	bytes := 0
	for _, f := range h.Frames {
		bytes += len(f.Body)
	}
	if len(h.Frames) > HistoryFrames || bytes > HistoryBytes {
		t.Fatal("unbounded history")
	}
	s := &Server{History: func() (MetricsHistory, error) { return h, nil }}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/metrics/history", nil))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var got MetricsHistory
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || len(got.Frames) != len(h.Frames) {
		t.Fatal(err)
	}
}

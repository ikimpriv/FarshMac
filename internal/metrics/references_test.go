package metrics

import (
	"testing"
	"time"
)

func TestPacketReferencesIndependentOfSelection(t *testing.T) {
	s := NewSession("test", nil, nil, nil)
	defer s.Close()
	s.SetReferenceVisibility(true)
	now := time.Now()
	s.append("manifest", Sample{Name: "farshmac_container_request_cpu_cores", Value: 0.5}, now)
	s.append("manifest", Sample{Name: "farshmac_container_limit_memory_bytes", Value: 1024}, now.Add(-time.Minute))
	p := s.Packet(true)
	if len(p.References) != 1 || p.References[0].Value != 0.5 || p.References[0].Resource != "cpu_cores" {
		t.Fatalf("%+v", p.References)
	}
	s.SetReferenceVisibility(false)
	if len(s.Packet(true).References) != 0 {
		t.Fatal("multi-container selection exposes bounds")
	}
	if len(p.Series) != 0 {
		t.Fatal("references should not require selected time series")
	}
	if resource, _ := ReferenceKind("podtoast_filesystem_size_bytes"); resource != "" {
		t.Fatal("filesystem capacity is not an ephemeral budget")
	}
}

package terminal

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const HistorySegmentBytes int64 = 16 << 20
const HistoryDiskBytes int64 = 1 << 30
const HistoryPageLines = 256
const MaxHistoryBatch = 8 << 20

type HistoryStatus struct {
	First int64  `json:"first"`
	Total int64  `json:"total"`
	Bytes int64  `json:"bytes"`
	Error string `json:"error,omitempty"`
}
type HistoryPage struct {
	HistoryStatus
	Request int64             `json:"request"`
	Start   int64             `json:"start"`
	Lines   []json.RawMessage `json:"lines"`
}
type historySegment struct {
	first, count, bytes int64
	path                string
}

// History keeps only segment metadata in RAM. Row offsets live in index files,
// so a million short lines does not create a million-entry in-memory index.
type History struct {
	dataWriter, indexWriter *bufio.Writer
	mu                      sync.Mutex
	dir                     string
	segments                []historySegment
	data, index             *os.File
	status                  HistoryStatus
	segmentBytes, diskBytes int64
	closed                  bool
}

func NewHistory() (*History, error) { return newHistory(HistorySegmentBytes, HistoryDiskBytes) }
func newHistory(segmentBytes, diskBytes int64) (*History, error) {
	dir, err := os.MkdirTemp("", "farshmac-history-*")
	if err != nil {
		return nil, err
	}
	return &History{dir: dir, segmentBytes: segmentBytes, diskBytes: diskBytes}, nil
}
func (h *History) rotate() error {
	if h.data != nil {
		if err := h.flush(); err != nil {
			return err
		}
		h.data.Close()
		h.index.Close()
		h.data, h.index = nil, nil
	}
	path := filepath.Join(h.dir, fmt.Sprintf("%020d", h.status.Total))
	f, err := os.OpenFile(path+".rows", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	i, err := os.OpenFile(path+".idx", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		f.Close()
		os.Remove(path + ".rows")
		return err
	}
	h.data, h.index = f, i
	h.dataWriter = bufio.NewWriterSize(f, 64*1024)
	h.indexWriter = bufio.NewWriterSize(i, 16*1024)
	h.segments = append(h.segments, historySegment{first: h.status.Total, path: path})
	return nil
}
func (h *History) Append(lines []json.RawMessage) (HistoryStatus, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return h.status, os.ErrClosed
	}
	if h.status.Error != "" {
		return h.status, fmt.Errorf("%s", h.status.Error)
	}
	fail := func(err error) (HistoryStatus, error) { h.status.Error = err.Error(); return h.status, err }
	batch := 0
	for _, line := range lines {
		batch += len(line)
		if len(line) > 256<<10 || batch > MaxHistoryBatch || len(lines) > 20000 || !json.Valid(line) {
			return fail(fmt.Errorf("terminal history batch exceeds limits or is invalid"))
		}
	}
	for _, line := range lines {
		if h.data == nil || h.segments[len(h.segments)-1].bytes >= h.segmentBytes {
			if err := h.rotate(); err != nil {
				return fail(err)
			}
		}
		s := &h.segments[len(h.segments)-1]
		// Offsets address data bytes only; accounting also includes the index.
		offset := s.bytes - s.count*8
		var size [4]byte
		binary.LittleEndian.PutUint32(size[:], uint32(len(line)))
		var index [8]byte
		binary.LittleEndian.PutUint64(index[:], uint64(offset))
		if _, err := h.dataWriter.Write(size[:]); err != nil {
			return fail(err)
		}
		if _, err := h.dataWriter.Write(line); err != nil {
			return fail(err)
		}
		if _, err := h.indexWriter.Write(index[:]); err != nil {
			return fail(err)
		}
		s.count++
		s.bytes += int64(len(line) + 12)
		h.status.Total++
		h.status.Bytes += int64(len(line) + 12)
		for h.status.Bytes > h.diskBytes && len(h.segments) > 1 {
			old := h.segments[0]
			if err := os.Remove(old.path + ".rows"); err != nil {
				return fail(err)
			}
			if err := os.Remove(old.path + ".idx"); err != nil {
				return fail(err)
			}
			h.status.Bytes -= old.bytes
			h.segments = h.segments[1:]
			h.status.First = h.segments[0].first
		}
	}
	if err := h.flush(); err != nil {
		return fail(err)
	}
	return h.status, nil
}
func (h *History) Page(start int64, count int) (HistoryPage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p := HistoryPage{HistoryStatus: h.status, Lines: []json.RawMessage{}}
	if h.closed {
		return p, os.ErrClosed
	}
	count = max(1, min(HistoryPageLines, count))
	p.Start = max(h.status.First, min(start, max(h.status.First, h.status.Total-int64(count))))
	end := min(h.status.Total, p.Start+int64(count))
	for _, s := range h.segments {
		lo, hi := max(p.Start, s.first), min(end, s.first+s.count)
		if lo >= hi {
			continue
		}
		err := func() error {
			data, err := os.Open(s.path + ".rows")
			if err != nil {
				return err
			}
			defer data.Close()
			index, err := os.Open(s.path + ".idx")
			if err != nil {
				return err
			}
			defer index.Close()
			for row := lo; row < hi; row++ {
				var offset [8]byte
				if _, err = index.ReadAt(offset[:], (row-s.first)*8); err != nil {
					return err
				}
				at := int64(binary.LittleEndian.Uint64(offset[:]))
				var size [4]byte
				if _, err = data.ReadAt(size[:], at); err != nil {
					return err
				}
				n := binary.LittleEndian.Uint32(size[:])
				if n > 256<<10 {
					return fmt.Errorf("invalid history record")
				}
				line := make([]byte, n)
				if _, err = data.ReadAt(line, at+4); err != nil {
					return err
				}
				p.Lines = append(p.Lines, json.RawMessage(line))
			}
			return nil
		}()
		if err != nil {
			return p, err
		}
	}
	return p, nil
}
func (h *History) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	if h.data != nil {
		h.data.Close()
		h.index.Close()
	}
	return os.RemoveAll(h.dir)
}

func (h *History) flush() error {
	if h.dataWriter == nil {
		return nil
	}
	if err := h.dataWriter.Flush(); err != nil {
		return err
	}
	return h.indexWriter.Flush()
}

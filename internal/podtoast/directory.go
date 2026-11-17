package podtoast

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

var ErrDirectorySummaryUnsupported = errors.New("PodToast directory summaries are not supported")

type DirectorySummary struct {
	Files       int64  `json:"files"`
	Directories int64  `json:"directories"`
	Links       int64  `json:"links"`
	Bytes       int64  `json:"bytes"`
	Allocated   int64  `json:"allocated"`
	Unreadable  int64  `json:"unreadable"`
	Done        bool   `json:"done"`
	Error       string `json:"error,omitempty"`
}

// scanDirectory uses bounded batches and never follows symbolic links. Counts
// are per directory entry (hard links are not deduplicated).
func scanDirectory(ctx context.Context, root string, ignoreDSStore bool, emit func(DirectorySummary) error) (DirectorySummary, error) {
	var total DirectorySummary
	last := time.Now()
	var walk func(string, int) error
	walk = func(p string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 128 {
			total.Unreadable++
			return nil
		}
		info, err := os.Lstat(p)
		if err != nil {
			total.Unreadable++
			return nil
		}
		if depth > 0 {
			if info.IsDir() {
				total.Directories++
			} else {
				total.Files++
				total.Bytes += info.Size()
				if info.Mode()&os.ModeSymlink != 0 {
					total.Links++
				}
				if stat, ok := info.Sys().(*syscall.Stat_t); ok {
					total.Allocated += stat.Blocks * 512
				}
			}
		}
		if time.Since(last) >= 200*time.Millisecond {
			if err := emit(total); err != nil {
				return err
			}
			last = time.Now()
		}
		if !info.IsDir() {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			total.Unreadable++
			return nil
		}
		defer f.Close()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			entries, err := f.ReadDir(128)
			for _, entry := range entries {
				if ignoreDSStore && entry.Name() == ".DS_Store" {
					continue
				}
				if err := walk(filepath.Join(p, entry.Name()), depth+1); err != nil {
					return err
				}
			}
			if err == io.EOF {
				return nil
			}
			if err != nil {
				total.Unreadable++
				return nil
			}
		}
	}
	if err := emit(total); err != nil {
		return total, err
	}
	err := walk(root, 0)
	total.Done = true
	if err != nil {
		total.Error = err.Error()
	}
	return total, err
}

func (s *Server) directorySummary(w http.ResponseWriter, r *http.Request) {
	root := s.path(r.URL.Query().Get("path"))
	info, err := os.Lstat(root)
	if err != nil {
		fail(w, err)
		return
	}
	if !info.IsDir() {
		http.Error(w, "not a directory", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	enc := json.NewEncoder(w)
	emit := func(v DirectorySummary) error {
		if s.CheckTarget != nil {
			if err := s.CheckTarget(); err != nil {
				return err
			}
		}
		if err := enc.Encode(v); err != nil {
			return err
		}
		return http.NewResponseController(w).Flush()
	}
	total, _ := scanDirectory(r.Context(), root, r.URL.Query().Get("ignoreDSStore") == "1", emit)
	if r.Context().Err() == nil {
		_ = emit(total)
	}
}

func (f *FS) DirectorySummary(path string, ignoreDSStore bool, update func(DirectorySummary) error) (DirectorySummary, error) {
	var last DirectorySummary
	if !f.c.directorySummary {
		return last, ErrDirectorySummaryUnsupported
	}
	query := "/v1/directory-summary?path=" + url.QueryEscape(path)
	if ignoreDSStore {
		query += "&ignoreDSStore=1"
	}
	r, err := f.c.request(f.ctx, "GET", query, nil)
	if err != nil {
		return last, err
	}
	defer r.Body.Close()
	scanner := bufio.NewScanner(r.Body)
	scanner.Buffer(make([]byte, 4096), 16<<10)
	for scanner.Scan() {
		if err = json.Unmarshal(scanner.Bytes(), &last); err != nil {
			return last, err
		}
		if update != nil {
			if err = update(last); err != nil {
				return last, err
			}
		}
		if last.Done {
			if last.Error != "" {
				return last, errors.New(last.Error)
			}
			return last, nil
		}
	}
	if err = scanner.Err(); err == nil {
		err = io.ErrUnexpectedEOF
	}
	return last, err
}

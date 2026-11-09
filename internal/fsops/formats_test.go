package fsops

import (
	"archive/tar"
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArchiveFormatsRoundTrip(t *testing.T) {
	for _, ext := range []string{"zip", "jar", "war", "tar", "tar.gz", "tgz", "tar.bz2", "tbz", "tbz2", "tar.xz", "txz", "7z"} {
		t.Run(ext, func(t *testing.T) {
			base := t.TempDir()
			source := filepath.Join(base, "source ' каталог")
			os.Mkdir(source, 0700)
			payload := bytes.Repeat([]byte("binary\x00data\n"), 1000)
			os.WriteFile(filepath.Join(source, "-file\nname"), payload, 0755)
			os.Mkdir(filepath.Join(source, "empty"), 0755)
			os.Symlink("-file\nname", filepath.Join(source, "link"))
			archive := filepath.Join(base, "my.backup."+ext)
			o := NewOperation("create")
			if err := CreateArchive([]string{source}, archive, o); err != nil {
				t.Fatal(err)
			}
			if p := o.Snapshot(); p.Files != p.TotalFiles || p.Bytes != p.TotalBytes {
				t.Fatal("creation progress", p)
			}
			report, err := ArchiveContents(archive, true)
			if err != nil || !strings.Contains(report, "Archive integrity: OK") {
				t.Fatal(report, err)
			}
			out := filepath.Join(base, "out")
			o = NewOperation("extract")
			if err = ExtractArchive(archive, out, o); err != nil {
				t.Fatal(err)
			}
			actual, err := os.ReadFile(filepath.Join(out, filepath.Base(source), "-file\nname"))
			if err != nil || !bytes.Equal(actual, payload) {
				t.Fatal("content", err)
			}
			link, err := os.Readlink(filepath.Join(out, filepath.Base(source), "link"))
			if err != nil || link != "-file\nname" {
				t.Fatal("symlink", link, err)
			}
			if p := o.Snapshot(); p.Bytes != p.TotalBytes || p.Files != p.TotalFiles || p.Streaming {
				t.Fatal("extraction progress", p)
			}
			if err = ExtractArchive(archive, out); err == nil {
				t.Fatal("existing destination overwritten")
			}
			if err = CreateArchive([]string{source}, archive); err == nil {
				t.Fatal("existing archive overwritten")
			}
			if stem := ArchiveStem(filepath.Base(archive)); stem != "my.backup" {
				t.Fatal("compound extension", stem)
			}
		})
	}
}
func tarFixture(t *testing.T, headers []*tar.Header) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "fixture.tar")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	w := tar.NewWriter(f)
	for _, h := range headers {
		if err = w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			if _, err = w.Write(bytes.Repeat([]byte("x"), int(h.Size))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return file
}
func TestArchiveExtractionConfinesPathsAndRollsBack(t *testing.T) {
	for _, name := range []string{"../escaped", "/absolute", "nested/../../escape", "C:/outside", "a\\..\\escape"} {
		t.Run(name, func(t *testing.T) {
			file := tarFixture(t, []*tar.Header{{Name: "first", Mode: 0600, Size: 1}, {Name: name, Mode: 0600, Size: 1}})
			out := filepath.Join(t.TempDir(), "out")
			if err := ExtractArchive(file, out); err == nil {
				t.Fatal("unsafe entry accepted")
			}
			if _, err := os.Lstat(out); !os.IsNotExist(err) {
				t.Fatal("partial destination published", err)
			}
			entries, _ := os.ReadDir(filepath.Dir(out))
			if len(entries) != 0 {
				t.Fatal("staging left behind", entries)
			}
		})
	}
	for _, headers := range [][]*tar.Header{
		{{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "../../escaped"}},
		{{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/tmp"}, {Name: "link/escaped", Size: 1, Mode: 0600}},
		{{Name: "dup", Size: 1, Mode: 0600}, {Name: "dup", Size: 1, Mode: 0600}},
		{{Name: "pipe", Typeflag: tar.TypeFifo, Mode: 0600}},
		{{Name: "link", Typeflag: tar.TypeLink, Linkname: "../outside"}},
	} {
		file := tarFixture(t, headers)
		out := filepath.Join(t.TempDir(), "out")
		if err := ExtractArchive(file, out); err == nil {
			t.Fatal("unsafe links/types accepted", headers)
		}
		if _, err := os.Lstat(out); !os.IsNotExist(err) {
			t.Fatal("failed extraction published", err)
		}
	}
	file := tarFixture(t, []*tar.Header{{Name: "file", Mode: 0644, Size: 3}, {Name: "hard", Typeflag: tar.TypeLink, Linkname: "file"}})
	out := filepath.Join(t.TempDir(), "out")
	if err := ExtractArchive(file, out); err != nil {
		t.Fatal(err)
	}
	a, _ := os.Stat(filepath.Join(out, "file"))
	b, _ := os.Stat(filepath.Join(out, "hard"))
	if !os.SameFile(a, b) {
		t.Fatal("hardlink not restored")
	}
}
func TestArchiveCorruptionAndCancellation(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "source")
	os.WriteFile(source, []byte("payload"), 0600)
	for _, ext := range []string{"zip", "tar.gz", "tar.bz2", "tar.xz", "7z"} {
		file := filepath.Join(base, "bad."+ext)
		if err := CreateArchive([]string{source}, file); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(file)
		os.WriteFile(file, b[:len(b)/2], 0600)
		if _, err := ArchiveContents(file, true); err == nil {
			t.Fatal("truncated archive passed integrity test", ext)
		}
		if err := ExtractArchive(file, filepath.Join(base, "out-"+ext)); err == nil {
			t.Fatal("truncated archive extracted", ext)
		}
	}
	huge := filepath.Join(base, "huge")
	f, _ := os.Create(huge)
	f.Truncate(256 << 20)
	f.Close()
	o := NewOperation("create")
	dst := filepath.Join(base, "cancel.tar.xz")
	done := make(chan error, 1)
	go func() { done <- CreateArchive([]string{huge}, dst, o) }()
	deadline := time.After(5 * time.Second)
	for o.Snapshot().Bytes == 0 {
		select {
		case err := <-done:
			t.Fatalf("operation ended before cancellation: %v", err)
		case <-deadline:
			t.Fatal("writer never progressed")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	o.Cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrCancelled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Esc did not stop archive subprocess")
	}
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Fatal("cancelled creation published output", err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(base, ".farshmac-archive-*"))
	if len(leftovers) > 0 {
		t.Fatal(leftovers)
	}
	for _, action := range []func() error{func() error { return ExtractArchive("missing", filepath.Join(base, "no"), o) }, func() error { _, e := ArchiveContents("missing", true, o); return e }} {
		if err := action(); !errors.Is(err, ErrCancelled) {
			t.Fatal(err)
		}
	}
}

// A minimal uncompressed RAR4 fixture, assembled from the public format fields.
// No RAR writer is included in the application.
func TestRARReadAndExtract(t *testing.T) {
	data := []byte("RAR contents\n")
	name := []byte("hello.txt")
	var fixture bytes.Buffer
	fixture.WriteString("Rar!\x1a\x07\x00")
	block := func(kind byte, flags uint16, body []byte) {
		header := []byte{kind, byte(flags), byte(flags >> 8), byte(7 + len(body)), byte((7 + len(body)) >> 8)}
		header = append(header, body...)
		sum := crc32.ChecksumIEEE(header)
		fixture.Write([]byte{byte(sum), byte(sum >> 8)})
		fixture.Write(header)
	}
	block(0x73, 0, make([]byte, 6))
	body := new(bytes.Buffer)
	for _, v := range []any{uint32(len(data)), uint32(len(data)), uint8(3), crc32.ChecksumIEEE(data), uint32(0), uint8(20), uint8(0x30), uint16(len(name)), uint32(0100644)} {
		binary.Write(body, binary.LittleEndian, v)
	}
	body.Write(name)
	block(0x74, 0x8000, body.Bytes())
	fixture.Write(data)
	block(0x7b, 0, nil)
	base := t.TempDir()
	file := filepath.Join(base, "test.rar")
	os.WriteFile(file, fixture.Bytes(), 0600)
	report, err := ArchiveContents(file, true)
	if err != nil || !strings.Contains(report, "hello.txt") {
		t.Fatal(report, err)
	}
	out := filepath.Join(base, "out")
	if err = ExtractArchive(file, out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "hello.txt"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal(string(got), err)
	}
	if err = CreateArchive([]string{filepath.Join(out, "hello.txt")}, filepath.Join(base, "new.rar")); err == nil {
		t.Fatal("RAR creation unexpectedly allowed")
	}
}

func TestGZIPChecksumIsVerified(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "source")
	os.WriteFile(src, []byte("unchanged payload"), 0600)
	archive := filepath.Join(base, "test.tar.gz")
	if err := CreateArchive([]string{src}, archive); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(archive)
	data[len(data)-8] ^= 1 // CRC32 in the gzip footer, not its TAR payload.
	os.WriteFile(archive, data, 0600)
	if _, err := ArchiveContents(archive, true); err == nil {
		t.Fatal("gzip CRC mismatch ignored")
	}
	if err := ExtractArchive(archive, filepath.Join(base, "out")); err == nil {
		t.Fatal("bad gzip published")
	}
}

func TestArchiveLiveExtractionCancellation(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "source")
	f, _ := os.Create(source)
	f.Truncate(128 << 20)
	f.Close()
	archive := filepath.Join(base, "large.tar")
	if err := CreateArchive([]string{source}, archive); err != nil {
		t.Fatal(err)
	}
	for _, test := range []bool{false, true} {
		o := NewOperation("read")
		out := filepath.Join(base, "out")
		done := make(chan error, 1)
		go func() {
			if test {
				_, err := ArchiveContents(archive, true, o)
				done <- err
			} else {
				done <- ExtractArchive(archive, out, o)
			}
		}()
		deadline := time.After(5 * time.Second)
		for o.Snapshot().Bytes == 0 {
			select {
			case err := <-done:
				t.Fatalf("operation ended before cancellation: %v", err)
			case <-deadline:
				t.Fatal("reader never progressed")
			default:
				time.Sleep(time.Millisecond)
			}
		}
		o.Cancel()
		select {
		case err := <-done:
			if !errors.Is(err, ErrCancelled) {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Esc did not stop archive read")
		}
		if _, err := os.Lstat(out); !os.IsNotExist(err) {
			t.Fatal("cancelled extraction published", err)
		}
		leftover, _ := filepath.Glob(filepath.Join(base, ".farshmac-extract-*"))
		if len(leftover) > 0 {
			t.Fatal(leftover)
		}
	}
}

func TestEncryptedArchiveFailsWithoutPrompt(t *testing.T) {
	base := t.TempDir()
	os.WriteFile(filepath.Join(base, "file"), []byte("test"), 0600)
	file := filepath.Join(base, "encrypted.zip")
	cmd := exec.Command("/usr/bin/zip", "-q", "-P", "fixture-password", file, "file")
	cmd.Dir = base
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %s: %v", out, err)
	}
	o := NewOperation("encrypted")
	defer o.Cancel()
	done := make(chan error, 1)
	go func() { _, err := ArchiveContents(file, true, o); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("encrypted archive accepted without password")
		}
	case <-time.After(3 * time.Second):
		o.Cancel()
		<-done
		t.Fatal("hidden archive password prompt blocked operation")
	}
}

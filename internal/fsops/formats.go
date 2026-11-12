package fsops

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// Decoding is performed by macOS's libarchive-backed bsdtar, in a separate,
// cancellable process. It only emits a TAR stream; Go controls all disk writes.
const archiveEntryLimit = 100000
const archiveReportLimit = 4 << 20

func ArchiveFormat(name string) string {
	name = strings.ToLower(name)
	for _, item := range []struct{ suffix, format string }{
		{".tar.gz", "tar.gz"}, {".tgz", "tar.gz"}, {".tar.bz2", "tar.bz2"}, {".tbz2", "tar.bz2"}, {".tbz", "tar.bz2"},
		{".tar.xz", "tar.xz"}, {".txz", "tar.xz"}, {".tar", "tar"}, {".zip", "zip"}, {".jar", "zip"}, {".war", "zip"},
		{".7z", "7z"}, {".rar", "rar"}, {".cpio", "cpio"},
	} {
		if strings.HasSuffix(name, item.suffix) {
			return item.format
		}
	}
	return ""
}
func ArchiveStem(name string) string {
	for _, suffix := range []string{".tar.gz", ".tar.bz2", ".tar.xz", ".tbz2", ".tgz", ".tbz", ".txz", ".tar", ".zip", ".jar", ".war", ".7z", ".rar", ".cpio"} {
		if strings.HasSuffix(strings.ToLower(name), suffix) {
			if stem := name[:len(name)-len(suffix)]; stem != "" {
				return stem
			}
			return "archive"
		}
	}
	return strings.TrimSuffix(name, filepath.Ext(name))
}

type archiveErrors struct{ bytes.Buffer }

func (b *archiveErrors) Write(p []byte) (int, error) {
	n := len(p)
	left := 32768 - b.Len()
	if left > 0 {
		b.Buffer.Write(p[:min(left, n)])
	}
	return n, nil
}
func archiveCommand(o *Operation, args ...string) (*exec.Cmd, *archiveErrors) {
	c := exec.CommandContext(o.Context(), "/usr/bin/tar", args...)
	// No controlling terminal: encrypted archives cannot open a hidden password
	// prompt. Kill the entire process session on Esc, including compression helpers.
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "TAR_OPTIONS=") && !strings.HasPrefix(env, "COPYFILE_DISABLE=") {
			c.Env = append(c.Env, env)
		}
	}
	c.Env = append(c.Env, "COPYFILE_DISABLE=1")
	diagnostics := &archiveErrors{}
	c.Stderr = diagnostics
	return c, diagnostics
}
func archiveProcessError(err error, diagnostic *archiveErrors) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("archive operation failed (damaged, encrypted or unsupported archive): %w: %s", err, strings.TrimSpace(diagnostic.String()))
}
func withArchive(src string, o *Operation, visit func(*tar.Reader) error) error {
	if err := o.Check(); err != nil {
		return err
	}
	src, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	if err = verifyGZIP(src, o); err != nil {
		return err
	}
	// libarchive can recover ZIP local entries after the central directory is
	// truncated. Recovery is not a successful integrity check or extraction.
	if ArchiveFormat(src) == "zip" {
		f, e := os.Open(src)
		if e != nil {
			return e
		}
		info, e := f.Stat()
		if e != nil {
			f.Close()
			return e
		}
		z, e := zip.NewReader(archiveCheckedReaderAt{f, o}, info.Size())
		f.Close()
		if e != nil {
			return e
		}
		if len(z.File) > archiveEntryLimit {
			return fmt.Errorf("archive exceeds %d entries", archiveEntryLimit)
		}
	}

	// Preserve original names during conversion so Go can reject absolute/drive
	// paths instead of silently accepting bsdtar's rewritten names.
	c, diagnostics := archiveCommand(o, "-c", "-P", "-f", "-", "--format=pax", "@"+src)
	pipe, err := c.StdoutPipe()
	if err != nil {
		return err
	}
	if err = c.Start(); err != nil {
		pipe.Close()
		return err
	}
	err = visit(tar.NewReader(pipe))
	if err == nil {
		_, err = io.Copy(io.Discard, pipe)
	}
	if err != nil {
		_ = c.Cancel()
	}
	pipe.Close()
	processErr := c.Wait()
	if cancel := o.Check(); cancel != nil {
		return cancel
	}
	if err != nil {
		return err
	}
	return archiveProcessError(processErr, diagnostics)
}

// libarchive's gzip filter does not validate the gzip CRC. Verify it with Go
// before reporting success, including concatenated members and zero tape padding.
type archiveCheckedReaderAt struct {
	io.ReaderAt
	o *Operation
}

func (r archiveCheckedReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if err := r.o.Check(); err != nil {
		return 0, err
	}
	return r.ReaderAt.ReadAt(p, off)
}

type archiveCheckedReader struct {
	io.Reader
	o *Operation
}

func (r archiveCheckedReader) Read(p []byte) (int, error) {
	if err := r.o.Check(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}
func verifyGZIP(src string, o *Operation) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	b := bufio.NewReader(archiveCheckedReader{f, o})
	magic, _ := b.Peek(2)
	if len(magic) != 2 || magic[0] != 0x1f || magic[1] != 0x8b {
		return nil
	}
	o.Begin(src, 0)
	for {
		z, err := gzip.NewReader(b)
		if err != nil {
			return fmt.Errorf("gzip integrity: %w", err)
		}
		z.Multistream(false)
		_, err = io.Copy(io.Discard, archiveCheckedReader{z, o})
		z.Close()
		if err != nil {
			return fmt.Errorf("gzip integrity: %w", err)
		}
		next, err := b.Peek(1)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if next[0] == 0 {
			buf := make([]byte, 32768)
			for {
				n, err := b.Read(buf)
				for _, v := range buf[:n] {
					if v != 0 {
						return fmt.Errorf("nonzero data after gzip stream")
					}
				}
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
			}
		}
	}
}

func safeArchiveName(name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if name == "" || strings.ContainsAny(name, "\\\x00") || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") || (len(clean) >= 2 && clean[1] == ':') {
		return "", fmt.Errorf("unsafe archive path: %q", name)
	}
	return clean, nil
}
func checkArchiveDestination(paths []string, dst string, o *Operation) error {
	if err := o.Check(); err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no files selected")
	}
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("archive already exists: %s", dst)
	} else if !os.IsNotExist(err) {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(dst))
	if err != nil {
		return err
	}
	names := map[string]bool{}
	for _, src := range paths {
		name := filepath.Base(src)
		if names[name] {
			return fmt.Errorf("duplicate archive root: %s", name)
		}
		names[name] = true
		info, err := os.Lstat(src)
		if err != nil {
			return err
		}
		if info.IsDir() {
			real, err := filepath.EvalSymlinks(src)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(real, parent)
			if err != nil {
				return err
			}
			if rel == "." || rel != ".." && !strings.HasPrefix(rel, "../") {
				return fmt.Errorf("archive destination is inside the source directory")
			}
		}
	}
	return nil
}
func CreateArchive(paths []string, dst string, ops ...*Operation) error {
	o := operation(ops)
	if err := checkArchiveDestination(paths, dst, o); err != nil {
		return err
	}
	format := ArchiveFormat(dst)
	if format == "zip" {
		return CreateZIP(paths, dst, o)
	}
	args := []string{"-c", "-f", "-", "--format=pax"}
	switch format {
	case "tar":
	case "tar.gz":
		args = append(args, "-z")
	case "tar.bz2":
		args = append(args, "-j")
	case "tar.xz":
		args = append(args, "-J")
	case "7z":
		args[3] = "--format=7zip"
	default:
		return fmt.Errorf("creation supports .zip, .tar, .tar.gz, .tar.bz2, .tar.xz and .7z")
	}
	if err := o.Scan(paths); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(dst), ".farshmac-archive-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	in, out := io.Pipe()
	args[2] = f.Name() // A real output file avoids bsdtar padding compressed data to tape blocks.
	c, diagnostics := archiveCommand(o, append(args, "@-")...)
	c.Stdin = in
	if err = c.Start(); err != nil {
		in.Close()
		out.Close()
		return err
	}
	done := make(chan error, 1)
	go func() { e := c.Wait(); in.CloseWithError(io.ErrClosedPipe); done <- e }()
	writer := tar.NewWriter(out)
	err = writeArchiveSources(writer, paths, o)
	if err == nil {
		err = writer.Close()
	}
	out.CloseWithError(err)
	if err != nil {
		_ = c.Cancel()
	}
	processErr := <-done
	if cancel := o.Check(); cancel != nil {
		return cancel
	}
	if processErr != nil {
		return archiveProcessError(processErr, diagnostics)
	}
	if err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = o.Check(); err != nil {
		return err
	}
	return os.Link(f.Name(), dst)
}

// CreatableArchive reports whether CreateArchive can write this format.
func CreatableArchive(name string) bool {
	switch ArchiveFormat(name) {
	case "zip", "tar", "tar.gz", "tar.bz2", "tar.xz", "7z":
		return true
	}
	return false
}

// openArchiveSource opens a file before its header is written, so a skipped
// unreadable file is simply left out of the archive.
func openArchiveSource(path string, o *Operation) (*os.File, error) {
	var in *os.File
	err := o.Attempt("read", path, "", func() error { var e error; in, e = os.Open(path); return e })
	return in, err
}

func writeArchiveSources(w *tar.Writer, paths []string, o *Operation) error {
	count := 0
	for _, root := range paths {
		err := WalkDir(root, o, func(path string, d fs.DirEntry, err error) error {
			if errors.Is(err, ErrSkipped) {
				return nil // An unreadable folder the user chose to skip stays empty.
			}
			if err != nil {
				return err
			}
			if err = o.Check(); err != nil {
				return err
			}
			count++
			if count > archiveEntryLimit {
				return fmt.Errorf("archive exceeds %d entries", archiveEntryLimit)
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if !info.IsDir() && !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("unsupported special file: %s", path)
			}
			target := ""
			if info.Mode()&os.ModeSymlink != 0 {
				target, err = os.Readlink(path)
				if err != nil {
					return err
				}
			}
			var in *os.File
			if info.Mode().IsRegular() {
				if in, err = openArchiveSource(path, o); errors.Is(err, ErrSkipped) {
					return nil
				} else if err != nil {
					return err
				}
				defer in.Close()
			}
			h, err := tar.FileInfoHeader(info, target)
			if err != nil {
				return err
			}
			h.Name, err = filepath.Rel(filepath.Dir(root), path)
			if err != nil {
				return err
			}
			h.Name = filepath.ToSlash(h.Name)
			h.Format = tar.FormatPAX
			if err = w.WriteHeader(h); err != nil {
				return err
			}
			o.Begin(path, info.Size())
			if in != nil {
				if _, err = copyProgress(w, in, o); err != nil {
					return err
				}
			}
			o.Complete(path)
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// ExtractArchive stages in a private sibling directory and atomically publishes
// only after all entries and decoder checks pass. Links are created last.
func ExtractArchive(src, dst string, ops ...*Operation) error {
	return extractArchive(src, dst, nil, operation(ops))
}
func extractArchive(src, dst string, include func(string) bool, o *Operation) error {
	if o != nil && o.IgnoreDSStore {
		selected := include
		include = func(name string) bool {
			return !o.ignoredArchivePath(name) && (selected == nil || selected(name))
		}
	}
	if err := o.Check(); err != nil {
		return err
	}
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("destination already exists: %s", dst)
	} else if !os.IsNotExist(err) {
		return err
	}
	staging, err := os.MkdirTemp(filepath.Dir(dst), ".farshmac-extract-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	root, err := os.OpenRoot(staging)
	if err != nil {
		return err
	}
	defer root.Close()
	seen := map[string]byte{}
	var links, dirs []*tar.Header
	o.Streaming()
	err = withArchiveSelection(src, o, include, func(r *tar.Reader) error {
		for count := 0; ; count++ {
			if err := o.Check(); err != nil {
				return err
			}
			h, err := r.Next()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			if count >= archiveEntryLimit {
				return fmt.Errorf("archive exceeds %d entries", archiveEntryLimit)
			}
			name, err := safeArchiveName(h.Name)
			if err != nil {
				return err
			}
			h.Name = name
			if name == "." && h.Typeflag != tar.TypeDir {
				return fmt.Errorf("invalid root archive entry")
			}
			if old, ok := seen[name]; ok && (old != tar.TypeDir || h.Typeflag != tar.TypeDir) {
				return fmt.Errorf("duplicate archive entry: %s", name)
			}
			seen[name] = h.Typeflag
			if include != nil && !include(name) {
				continue
			}
			o.Add(name, h.Size)
			o.Begin(name, h.Size)
			switch h.Typeflag {
			case tar.TypeDir:
				if err = root.MkdirAll(name, 0700); err != nil {
					return err
				}
				dirs = append(dirs, h)
			case tar.TypeReg, tar.TypeRegA:
				if err = root.MkdirAll(filepath.Dir(name), 0700); err != nil {
					return err
				}
				f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(h.Mode).Perm()|0600)
				if err != nil {
					return err
				}
				_, err = copyProgress(f, r, o)
				closeErr := f.Close()
				if err != nil {
					return err
				}
				if closeErr != nil {
					return closeErr
				}
				if !h.ModTime.IsZero() {
					if err = root.Chtimes(name, h.ModTime, h.ModTime); err != nil {
						return err
					}
				}
			case tar.TypeSymlink, tar.TypeLink:
				if h.Typeflag == tar.TypeSymlink {
					if filepath.IsAbs(h.Linkname) || strings.ContainsAny(h.Linkname, "\\\x00") || (len(h.Linkname) >= 2 && h.Linkname[1] == ':') {
						return fmt.Errorf("unsafe archive link: %q", h.Linkname)
					}
					if _, err = safeArchiveName(filepath.Join(filepath.Dir(name), h.Linkname)); err != nil {
						return err
					}
				} else {
					if _, err = safeArchiveName(h.Linkname); err != nil {
						return err
					}
				}
				if err = root.MkdirAll(filepath.Dir(name), 0700); err != nil {
					return err
				}
				links = append(links, h)
			default:
				return fmt.Errorf("unsupported archive entry type: %s", name)
			}
			o.Complete(name)
		}
	})
	if err != nil {
		return err
	}
	// Restore directory metadata before adding symlinks. No later data writes can
	// traverse a link planted by an archive, and os.Root confines all operations.
	for i := len(dirs) - 1; i >= 0; i-- {
		if err = o.Check(); err != nil {
			return err
		}
		h := dirs[i]
		if err = root.Chmod(h.Name, os.FileMode(h.Mode).Perm()|0700); err != nil {
			return err
		}
	}
	for _, h := range links {
		if err = o.Check(); err != nil {
			return err
		}
		if h.Typeflag == tar.TypeLink {
			info, err := root.Lstat(h.Linkname)
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("hard link target is not a regular file: %s", h.Linkname)
			}
			if err = root.Link(h.Linkname, h.Name); err != nil {
				return err
			}
		}
	}
	for _, h := range links {
		if h.Typeflag == tar.TypeSymlink {
			if err = o.Check(); err != nil {
				return err
			}
			if err = root.Symlink(h.Linkname, h.Name); err != nil {
				return err
			}
		}
	}
	for _, h := range links {
		if h.Typeflag == tar.TypeSymlink {
			if err = o.Check(); err != nil {
				return err
			}
			if _, err = root.Stat(h.Name); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("unsafe archive link %s: %w", h.Name, err)
			}
		}
	}
	if err = o.Check(); err != nil {
		return err
	}
	o.Ready()
	root.Close()
	return Move(staging, dst)
}
func ArchiveContents(src string, test bool, ops ...*Operation) (string, error) {
	o := operation(ops)
	o.Streaming()
	var report strings.Builder
	truncated := false
	err := withArchive(src, o, func(r *tar.Reader) error {
		for count := 0; ; count++ {
			if err := o.Check(); err != nil {
				return err
			}
			h, err := r.Next()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			if count >= archiveEntryLimit {
				return fmt.Errorf("archive exceeds %d entries", archiveEntryLimit)
			}
			o.Add(h.Name, h.Size)
			o.Begin(h.Name, h.Size)
			if report.Len() < archiveReportLimit && count < 10000 {
				fmt.Fprintf(&report, "%12d  %s", h.Size, strings.Map(func(r rune) rune {
					if r < 32 || r == 127 {
						return '�'
					}
					return r
				}, h.Name))
				if h.Typeflag == tar.TypeDir {
					report.WriteString("/")
				}
				if h.Linkname != "" {
					fmt.Fprintf(&report, " -> %q", h.Linkname)
				}
				report.WriteByte('\n')
			} else {
				truncated = true
			}
			// Consume every member even for listing: the process verifies decoding and
			// checksums and never accumulates an expanded archive in RAM or on disk.
			if _, err = copyProgress(io.Discard, r, o); err != nil {
				return err
			}
			o.Complete(h.Name)
		}
	})
	if err != nil {
		return "", err
	}
	o.Ready()
	if truncated {
		report.WriteString("\nListing truncated (10,000 rows / 4 MiB); all entries were read.\n")
	}
	if test {
		report.WriteString("\nArchive integrity: OK (all entries decoded; available format checksums verified)\n")
	}
	return report.String(), nil
}

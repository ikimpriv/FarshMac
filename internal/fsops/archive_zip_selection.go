package fsops

import (
	"archive/tar"
	"archive/zip"
	"fmt"
	"io"
	"os"
)

// ZIP's central directory permits reading only selected members. Adapt those
// members to the same TAR extraction validator through a bounded pipe; no full
// archive expansion or second in-memory copy is needed. Unusual compression
// methods retain the system libarchive fallback.
func withArchiveSelection(src string, o *Operation, include func(string) bool, visit func(*tar.Reader) error) error {
	if include == nil || ArchiveFormat(src) != "zip" {
		return withArchive(src, o, visit)
	}
	if err := o.Check(); err != nil {
		return err
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	z, err := zip.NewReader(archiveCheckedReaderAt{f, o}, info.Size())
	if err != nil {
		return err
	}
	if len(z.File) > archiveEntryLimit {
		return fmt.Errorf("archive entry limit")
	}
	var members []*zip.File
	for _, entry := range z.File {
		if err = o.Check(); err != nil {
			return err
		}
		name, e := safeArchiveName(entry.Name)
		if e != nil {
			return e
		}
		if !include(name) {
			continue
		}
		if entry.Method != zip.Store && entry.Method != zip.Deflate {
			return withArchive(src, o, visit)
		}
		members = append(members, entry)
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := func() error {
			tarWriter := tar.NewWriter(writer)
			for _, entry := range members {
				if e := o.Check(); e != nil {
					return e
				}
				fi := entry.FileInfo()
				if entry.UncompressedSize64 > 1<<63-1 {
					return fmt.Errorf("archive member too large")
				}
				link := ""
				if fi.Mode()&os.ModeSymlink != 0 {
					r, e := entry.Open()
					if e != nil {
						return e
					}
					data, e := io.ReadAll(io.LimitReader(archiveCheckedReader{r, o}, 4097))
					r.Close()
					if e != nil {
						return e
					}
					if len(data) > 4096 {
						return fmt.Errorf("archive link too long")
					}
					link = string(data)
				}
				h, e := tar.FileInfoHeader(fi, link)
				if e != nil {
					return e
				}
				h.Name = entry.Name
				h.Format = tar.FormatPAX
				if e = tarWriter.WriteHeader(h); e != nil {
					return e
				}
				if fi.Mode().IsRegular() {
					r, e := entry.Open()
					if e != nil {
						return e
					}
					_, e = io.Copy(tarWriter, archiveCheckedReader{r, o})
					r.Close()
					if e != nil {
						return e
					}
				}
			}
			return tarWriter.Close()
		}()
		writer.CloseWithError(err)
		done <- err
	}()
	err = visit(tar.NewReader(reader))
	if err == nil {
		_, err = io.Copy(io.Discard, reader)
	}
	reader.CloseWithError(io.ErrClosedPipe)
	produced := <-done
	if e := o.Check(); e != nil {
		return e
	}
	if err != nil {
		return err
	}
	return produced
}

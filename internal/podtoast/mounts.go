package podtoast

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// MountLocation is mount identity only; a Kubernetes PVC/emptyDir classification
// requires the pod specification and must not be inferred from ext4/tmpfs alone.
type MountLocation struct {
	Path       string `json:"path"`
	Filesystem string `json:"filesystem"`
}

func readMountLocations(r io.Reader) []MountLocation {
	data, err := io.ReadAll(io.LimitReader(r, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil
	}
	scan := bufio.NewScanner(bytes.NewReader(data))
	scan.Buffer(make([]byte, 4096), 128<<10)
	result := []MountLocation{}
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	for scan.Scan() {
		left, right, ok := strings.Cut(scan.Text(), " - ")
		a, b := strings.Fields(left), strings.Fields(right)
		if !ok || len(a) < 6 || len(b) < 2 {
			continue
		}
		result = append(result, MountLocation{Path: path.Clean(unescape.Replace(a[4])), Filesystem: b[0]})
		if len(result) >= 4096 {
			return nil
		}
	}
	if scan.Err() != nil {
		return nil
	}
	return result
}
func mountAt(mounts []MountLocation, p string) *MountLocation {
	p = path.Clean("/" + p)
	var best *MountLocation
	for i := range mounts {
		m := &mounts[i]
		if p == m.Path || m.Path == "/" || strings.HasPrefix(p, m.Path+"/") {
			if best == nil || len(m.Path) >= len(best.Path) {
				best = m
			}
		}
	}
	return best
}
func (s *Server) listingMounts() []MountLocation {
	if s.TargetPID < 1 {
		return nil
	}
	f, e := os.Open(s.path("/proc/" + strconv.Itoa(s.TargetPID) + "/mountinfo"))
	if e != nil {
		return nil
	}
	defer f.Close()
	return readMountLocations(f)
}
func (s *Server) listingPath(p string) string {
	// Resolve the directory once, so browsing a symlink into a volume keeps the
	// target mount classification. Entries themselves remain lstat-based.
	if resolved, e := filepath.EvalSymlinks(p); e == nil {
		if relative, e := filepath.Rel(s.Root, resolved); e == nil && !strings.HasPrefix(relative, "..") {
			return "/" + filepath.ToSlash(relative)
		}
	}
	relative, e := filepath.Rel(s.Root, p)
	if e != nil {
		return "/"
	}
	return "/" + filepath.ToSlash(relative)
}

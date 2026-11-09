package fsops

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Run with -run '^$' -bench BenchmarkSmallFiles -benchtime=1x.
// Both paths copy the same 2,000 4-KiB files on the same volume.
func BenchmarkSmallFiles(b *testing.B) {
	root := b.TempDir()
	src := filepath.Join(root, "source")
	if err := os.Mkdir(src, 0755); err != nil {
		b.Fatal(err)
	}
	data := make([]byte, 4096)
	for i := 0; i < 2000; i++ {
		if err := os.WriteFile(filepath.Join(src, fmt.Sprint(i)), data, 0644); err != nil {
			b.Fatal(err)
		}
	}
	for _, system := range []bool{false, true} {
		b.Run(map[bool]string{false: "FarshMac", true: "SystemCP"}[system], func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(2000 * 4096)
			for i := 0; i < b.N; i++ {
				dst := filepath.Join(root, fmt.Sprintf("copy-%t-%d", system, i))
				var err error
				if system {
					err = exec.Command("/bin/cp", "-Rp", src, dst).Run()
				} else {
					err = Copy(src, dst)
				}
				if err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				os.RemoveAll(dst)
				b.StartTimer()
			}
		})
	}
}

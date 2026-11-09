package ui

import (
	"farshmac/internal/fsops"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkFolderSizesLegacy(b *testing.B) {
	root := b.TempDir()
	for i := 0; i < 16; i++ {
		dir := filepath.Join(root, fmt.Sprint(i))
		os.Mkdir(dir, 0700)
		for j := 0; j < 256; j++ {
			os.WriteFile(filepath.Join(dir, fmt.Sprint(j)), []byte("test"), 0600)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		o := fsops.NewOperation("size")
		var size int64
		err := fsops.WalkDir(root, o, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			o.Add(p, 0)
			if !d.IsDir() {
				info, e := d.Info()
				if e != nil {
					return e
				}
				size += info.Size()
			}
			return nil
		})
		if err != nil || size != 16384 {
			b.Fatal(size, err)
		}
		o.Cancel()
	}
}

func BenchmarkFolderSizesStreaming(b *testing.B) {
	root := b.TempDir()
	var names []string
	for i := 0; i < 16; i++ {
		name := fmt.Sprint(i)
		names = append(names, name)
		dir := filepath.Join(root, name)
		os.Mkdir(dir, 0700)
		for j := 0; j < 256; j++ {
			os.WriteFile(filepath.Join(dir, fmt.Sprint(j)), []byte("test"), 0600)
		}
	}
	for _, workers := range []int{1, 4} {
		b.Run(fmt.Sprintf("workers-%d", workers), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				o := fsops.NewOperation("size")
				sizes, err := measureFolders(root, names, o, fsops.WalkDir, workers)
				var size int64
				for _, s := range sizes {
					size += s.Bytes
				}
				if err != nil || size != 16384 {
					b.Fatal(size, err)
				}
				o.Cancel()
			}
		})
	}
}

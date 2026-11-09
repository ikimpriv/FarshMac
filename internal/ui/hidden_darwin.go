package ui

import (
	"os"
	"strings"
	"syscall"
)

func hiddenEntry(name string, info os.FileInfo) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	if info != nil {
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			return stat.Flags&0x00008000 != 0 // UF_HIDDEN, sys/stat.h
		}
	}
	return false
}

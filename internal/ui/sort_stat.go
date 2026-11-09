package ui

import (
	"cmp"
	"syscall"
)

func compareStat(a, b Entry, mode string) int {
	if a.Info == nil || b.Info == nil {
		return 0
	}
	x, xok := a.Info.Sys().(*syscall.Stat_t)
	y, yok := b.Info.Sys().(*syscall.Stat_t)
	if !xok || !yok {
		return 0
	}
	switch mode {
	case "Owner":
		return cmp.Compare(x.Uid, y.Uid)
	case "Created":
		if n := cmp.Compare(x.Birthtimespec.Sec, y.Birthtimespec.Sec); n != 0 {
			return n
		}
		return cmp.Compare(x.Birthtimespec.Nsec, y.Birthtimespec.Nsec)
	case "Accessed":
		if n := cmp.Compare(x.Atimespec.Sec, y.Atimespec.Sec); n != 0 {
			return n
		}
		return cmp.Compare(x.Atimespec.Nsec, y.Atimespec.Nsec)
	}
	return 0
}

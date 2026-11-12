package ui

import (
	"errors"
	"io/fs"
	"syscall"
)

const (
	privacyDeniedFooter = "Access denied by macOS privacy protection"
	diskAccessHint      = "Grant Full Disk Access: Options → Full Disk Access…"
)

// describeReadError turns a local folder read error into the panel footer
// text and an optional status hint. macOS privacy protection (TCC) reports
// EPERM, which only Full Disk Access can fix; ordinary Unix permissions report
// EACCES. Remote and archive errors are not syscall errnos and pass through.
func describeReadError(path string, err error) (footer, hint string) {
	if err == nil {
		return "", ""
	}
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return err.Error(), ""
	}
	if path == "" {
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			path = pathErr.Path
		}
	}
	switch errno {
	case syscall.EPERM:
		return privacyDeniedFooter, diskAccessHint
	case syscall.EACCES:
		if path == "" {
			return "Permission denied", ""
		}
		return "Permission denied: " + path, ""
	}
	return err.Error(), ""
}

// specialFileKind names file types Quick view cannot preview. Regular files
// and directories return "".
func specialFileKind(mode fs.FileMode) string {
	switch {
	case mode&fs.ModeNamedPipe != 0:
		return "Named pipe"
	case mode&fs.ModeSocket != 0:
		return "Socket"
	case mode&fs.ModeCharDevice != 0:
		return "Character device"
	case mode&fs.ModeDevice != 0:
		return "Block device"
	case mode&fs.ModeSymlink != 0:
		return "Symbolic link"
	case mode.IsRegular() || mode.IsDir():
		return ""
	}
	return "Special file"
}

//go:build !linux

package app

import (
	"io/fs"
)

// isPlainFolderMountPoint is a no-op off Linux.
func isPlainFolderMountPoint(info fs.FileInfo) bool {
	return false
}

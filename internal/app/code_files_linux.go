package app

import (
	"io/fs"
	"syscall"
)

// isPlainFolderMountPoint reports whether a directory is a filesystem mount
// root, identified on Linux by its inode being 1: procfs, sysfs, devtmpfs,
// tmpfs, cgroups and every other mounted filesystem. A plain-folder workspace
// opened on a chroot snapshot root has such mounts directly inside the root,
// and their trees are noise, not project files.
func isPlainFolderMountPoint(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Ino == 1
}

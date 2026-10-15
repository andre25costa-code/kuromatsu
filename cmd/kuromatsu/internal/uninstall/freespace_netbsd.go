//go:build netbsd

package uninstall

import "golang.org/x/sys/unix"

// freeSpace returns the bytes available to the user on dir's filesystem
// (NetBSD has statvfs instead of statfs).
func freeSpace(dir string) (uint64, error) {
	var st unix.Statvfs_t
	if err := unix.Statvfs(dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Frsize), nil
}

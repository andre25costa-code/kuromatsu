//go:build !windows && !netbsd

package uninstall

import "golang.org/x/sys/unix"

// freeSpace returns the bytes available to the user on dir's filesystem.
func freeSpace(dir string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, err
	}
	// Field types differ per OS and arch (Bavail is int64 on FreeBSD, Bsize
	// int32 on linux/arm); both values are non-negative.
	avail := uint64(st.Bavail) //nolint:unconvert // already uint64 on some platforms only
	return avail * uint64(st.Bsize), nil
}

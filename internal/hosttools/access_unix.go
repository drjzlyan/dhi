//go:build unix

package hosttools

import "golang.org/x/sys/unix"

// writable asks the kernel whether DHI could write dir, without writing.
func writable(dir string) bool { return unix.Access(dir, unix.W_OK) == nil }

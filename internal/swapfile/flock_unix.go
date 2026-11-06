//go:build unix

package swapfile

import (
	"errors"
	"os"
	"syscall"
)

// tryLock takes an exclusive lock without waiting; held is true when
// another open file description owns it.
func tryLock(f *os.File) (held bool, err error) {
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		switch {
		case err == nil:
			return false, nil
		case errors.Is(err, syscall.EINTR):
		case errors.Is(err, syscall.EWOULDBLOCK):
			return true, nil
		default:
			return false, err
		}
	}
}

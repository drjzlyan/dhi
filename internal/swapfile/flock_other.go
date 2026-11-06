//go:build !unix

package swapfile

import (
	"errors"
	"os"
)

// tryLock is unavailable here; swaps fall back to the pid check.
func tryLock(*os.File) (bool, error) {
	return false, errors.New("swapfile: no file locks on this platform")
}

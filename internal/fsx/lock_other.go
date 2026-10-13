//go:build !unix

package fsx

import "os"

// Non-unix builds (not a release platform) run without cross-process
// locks; in-process mutexes still serialise each store.
func lockFile(*os.File) error   { return nil }
func unlockFile(*os.File) error { return nil }

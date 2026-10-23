//go:build !unix

package gitcore

import "os/exec"

func detach(*exec.Cmd) {}

//go:build unix

package gitcore

import (
	"os/exec"
	"syscall"
)

// detach starts git in a new session with no controlling terminal: ssh
// then cannot open DHI's tty to ask for a passphrase (which would draw
// over the TUI); it fails visibly instead, or uses SSH_ASKPASS.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

package toolchain

import "os/exec"

func osRun(path string) ([]byte, error) {
	return exec.Command(path).CombinedOutput()
}

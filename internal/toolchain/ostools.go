package toolchain

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// OS seams (ADR-0042). DHI is hermetic (ADR-0005), with two narrow
// exceptions that only the operating system can provide: putting text on
// the system clipboard (macOS Terminal.app ignores OSC 52) and opening a
// login page in the user's browser. Both use the OS's own tool, are
// optional — callers always have a fallback (OSC 52, a printed URL) — and
// live here so nothing else in DHI shells out to the host.

// ErrNoOSTool reports that the platform has no tool for the request.
var ErrNoOSTool = errors.New("no system tool available")

// osToolTimeout bounds a clipboard or browser call; neither may hang DHI.
const osToolTimeout = 5 * time.Second

// clipboardArgv and openArgv are swapped by tests.
var (
	clipboardArgv = defaultClipboardArgv
	openArgv      = defaultOpenArgv
)

// CopyToClipboard puts text on the system clipboard with the platform's
// own tool: pbcopy on macOS, wl-copy (Wayland) or xclip (X11) on Linux.
func CopyToClipboard(text string) error {
	argv := clipboardArgv()
	if len(argv) == 0 {
		return ErrNoOSTool
	}
	ctx, cancel := context.WithTimeout(context.Background(), osToolTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// OpenURL opens url in the user's browser (open on macOS, xdg-open on
// Linux). Callers also show the URL, so a failure never blocks a login.
func OpenURL(url string) error {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return errors.New("refusing to open a non-web URL")
	}
	argv := openArgv(url)
	if len(argv) == 0 {
		return ErrNoOSTool
	}
	ctx, cancel := context.WithTimeout(context.Background(), osToolTimeout)
	defer cancel()
	return exec.CommandContext(ctx, argv[0], argv[1:]...).Run()
}

func defaultClipboardArgv() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"/usr/bin/pbcopy"}
	case "linux":
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			if p, err := exec.LookPath("wl-copy"); err == nil {
				return []string{p}
			}
		}
		if os.Getenv("DISPLAY") != "" {
			if p, err := exec.LookPath("xclip"); err == nil {
				return []string{p, "-selection", "clipboard"}
			}
		}
	}
	return nil
}

func defaultOpenArgv(url string) []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"/usr/bin/open", url}
	case "linux":
		if p, err := exec.LookPath("xdg-open"); err == nil {
			return []string{p, url}
		}
	}
	return nil
}

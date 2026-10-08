package settings

import "github.com/drjzlyan/dhi/internal/tui/surfaces"

// The shell relies on this seam to leave digits, "?" and tab to text
// inputs (F-041); losing it silently would reintroduce the hijack.
var _ surfaces.InputCapturer = (*Model)(nil)

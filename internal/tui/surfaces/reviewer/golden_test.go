package reviewer

import (
	"testing"

	"github.com/drjzlyan/dhi/internal/testutil/golden"
)

func goldenCompare(t *testing.T, name, actual string) {
	t.Helper()
	golden.Snapshot(t, name, actual)
}

// Goldens pin the ANSI-stripped layout of each section. Regenerate
// deliberately after an intentional visual change:
//
//	DHI_UPDATE_GOLDENS=1 go test ./internal/tui/surfaces/reviewer/
func TestGoldenReviewsEmpty(t *testing.T) {
	m, _, _, _ := newSurface(t)
	goldenCompare(t, "reviews-empty", m.View())
}

func TestGoldenNewReviewModal(t *testing.T) {
	m, _, _, _ := newSurface(t)
	m.HandleKey("n")
	goldenCompare(t, "new-review-modal", m.View())
}

func TestGoldenFilesList(t *testing.T) {
	m, _, _, _ := newSurface(t)
	startBranchReview(t, m, m.svc.Store())
	goldenCompare(t, "files-list", m.View())
}

func TestGoldenDiffUnified(t *testing.T) {
	m, _, _, _ := newSurface(t)
	startBranchReview(t, m, m.svc.Store())
	m.HandleKey("]") // FILES → DIFF
	goldenCompare(t, "diff-unified", m.View())
}

func TestGoldenDiffSideBySide(t *testing.T) {
	m, _, _, _ := newSurface(t)
	startBranchReview(t, m, m.svc.Store())
	m.HandleKey("]")
	m.HandleKey("\\")
	goldenCompare(t, "diff-side-by-side", m.View())
}

package workspace

import (
	"testing"

	"github.com/drjzlyan/dhi/internal/tasks"
)

// F-065: the replay header shows the run's prompt-cache share, and says
// nothing when the CLI reported none.
func TestReplayCacheText(t *testing.T) {
	if got := cacheText(tasks.Run{TokensIn: 300, CacheRead: 2700}); got != " · 90% cached" {
		t.Fatalf("cacheText = %q", got)
	}
	if got := cacheText(tasks.Run{TokensIn: 300}); got != "" {
		t.Fatalf("no cache must render nothing, got %q", got)
	}
}

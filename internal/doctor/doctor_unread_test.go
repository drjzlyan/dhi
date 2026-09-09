package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUnreadStoreSuite(t *testing.T) {
	ws := setupWorkspaceRoot(t, true)

	// Missing file: fresh install, silent.
	if checks := UnreadStore(ws); checks != nil {
		t.Fatalf("missing file emitted %+v", checks)
	}

	// Healthy store: watermarks + an active snooze on a real channel
	// (seed one channel file so the bus lists #general).
	os.MkdirAll(filepath.Join(ws, ".dhi", "channels", "channels"), 0o755)
	os.WriteFile(filepath.Join(ws, ".dhi", "channels", "channels", "general.jsonl"),
		[]byte(`{"id":7,"channel":"#general","thread":0,"author":"scout","text":"@you ping","at":"2026-01-01T00:00:00Z"}`+"\n"), 0o644)
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	body := fmt.Sprintf(`{"schema":1,"channels":{"#general":7},"snoozes":[{"channel":"#general","messageID":2,"until":%q}]}`, future)
	os.WriteFile(filepath.Join(ws, ".dhi", "unread.json"), []byte(body), 0o644)
	checks := UnreadStore(ws)
	c, ok := statusOf(checks, "unread/store")
	if !ok || c.Status != OK || !strings.Contains(c.Detail, "1 watermark") {
		t.Fatalf("healthy = %+v (found=%v)", c, ok)
	}

	// Dangling snooze channel warns by name.
	dangling := `{"schema":1,"channels":{},"snoozes":[{"channel":"#ghost","messageID":9,"until":"` + future + `"}]}`
	os.WriteFile(filepath.Join(ws, ".dhi", "unread.json"), []byte(dangling), 0o644)
	checks = UnreadStore(ws)
	c, _ = statusOf(checks, "unread/store")
	if c.Status != Warn || !strings.Contains(c.Detail, "#ghost") {
		t.Fatalf("dangling = %+v", c)
	}

	// Expired snoozes are counted in the detail, not an error.
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	expired := `{"schema":1,"channels":{},"snoozes":[{"channel":"#general","messageID":1,"until":"` + past + `"}]}`
	os.WriteFile(filepath.Join(ws, ".dhi", "unread.json"), []byte(expired), 0o644)
	checks = UnreadStore(ws)
	c, _ = statusOf(checks, "unread/store")
	if c.Status != OK || !strings.Contains(c.Detail, "1 expired") {
		t.Fatalf("expired = %+v", c)
	}

	// Strict decode failures FAIL with the value named (F-011).
	os.WriteFile(filepath.Join(ws, ".dhi", "unread.json"), []byte(`{"schema":3,"channels":{}}`), 0o644)
	checks = UnreadStore(ws)
	c, _ = statusOf(checks, "unread/store")
	if c.Status != Fail || !strings.Contains(c.Detail, "schema 3") {
		t.Fatalf("bad schema = %+v", c)
	}
	os.WriteFile(filepath.Join(ws, ".dhi", "unread.json"), []byte(`{"schema":1,"channels":{},"junk":1}`), 0o644)
	checks = UnreadStore(ws)
	c, _ = statusOf(checks, "unread/store")
	if c.Status != Fail || !strings.Contains(c.Detail, "junk") {
		t.Fatalf("unknown key = %+v", c)
	}

	// The aggregate JSON report includes the row.
	if data, err := json.Marshal(Run("", ws)); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(string(data), "unread/store") {
		t.Fatalf("report missing unread/store: %s", data)
	}
}

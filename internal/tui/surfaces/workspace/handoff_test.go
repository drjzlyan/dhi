package workspace

import (
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// TestHandoffRoutes covers F-036: creating/assigning a card posts the
// brief where the work should start and dispatches it.
func TestHandoffRoutes(t *testing.T) {
	cases := []struct {
		name     string
		task     tasks.Task
		wantChan string
		wantText string
		wantNone bool
	}{
		{"team+assignee", tasks.Task{Slug: "a", Title: "Do A", Team: "web", Assignee: "bo"}, "#web", "@bo task a: Do A", false},
		{"team only reaches the lead", tasks.Task{Slug: "b", Title: "Do B", Team: "web"}, "#web", "task b: Do B", false},
		{"assignee only goes to DM", tasks.Task{Slug: "c", Title: "Do C", Assignee: "bo"}, "dm:bo", "task c: Do C", false},
		{"bound thread wins", tasks.Task{Slug: "d", Title: "Do D", Team: "web", Assignee: "bo", ThreadChannel: "#web", ThreadID: 7}, "#web", "@bo task d: Do D", false},
		{"nobody to hand to", tasks.Task{Slug: "e", Title: "Do E"}, "", "", true},
		{"human assignee is not dispatched", tasks.Task{Slug: "f", Title: "Do F", Assignee: bus.Human}, "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _, _ := newSurfaceWithBus(t)
			capT := &capturingTurns{posts: make(chan bus.Message, 2)}
			m.rt = capT
			m.handoff(c.task)
			select {
			case msg := <-capT.posts:
				if c.wantNone {
					t.Fatalf("unexpected dispatch %+v", msg)
				}
				if msg.Channel != c.wantChan || msg.Text != c.wantText || msg.Author != bus.Human {
					t.Fatalf("dispatch = %+v, want %s %q", msg, c.wantChan, c.wantText)
				}
				if c.task.ThreadID != 0 && msg.Thread != c.task.ThreadID {
					t.Fatalf("thread = %d, want %d", msg.Thread, c.task.ThreadID)
				}
			case <-time.After(200 * time.Millisecond):
				if !c.wantNone {
					t.Fatal("no dispatch")
				}
			}
		})
	}
}

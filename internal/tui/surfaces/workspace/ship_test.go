package workspace

import (
	"testing"

	"github.com/drjzlyan/dhi/internal/tasks"
)

// TestShipToastOnDone (F-093): a card reaching done raises one toast
// naming it; the first look (launch) never celebrates history.
func TestShipToastOnDone(t *testing.T) {
	m, ws := newSurface(t)
	store, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Create("old", "Already done", "", "")
	_ = store.SetStatus("old", tasks.Done)
	_ = store.Create("ship-docs", "Ship docs", "", "")
	m.taskStore = store

	if _, ok := m.TakeToast(); ok {
		t.Fatal("first look celebrated a card that was already done")
	}
	_ = store.SetStatus("ship-docs", tasks.Done)
	toast, ok := m.TakeToast()
	if !ok || toast.Title != "ship-docs shipped" {
		t.Fatalf("toast = %+v, %v; want ship-docs shipped", toast, ok)
	}
	if _, ok := m.TakeToast(); ok {
		t.Fatal("the same ship raised a second toast")
	}
}

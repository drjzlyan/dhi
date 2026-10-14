package workspace

import (
	"fmt"

	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/tui/kit"
)

// noticeShipped watches card statuses between frames and raises a toast
// the moment a card reaches done (F-093: the ship moment), whichever
// path moved it — a key, a bulk move, a review merge, an agent. The
// first look only records, so launching DHI never celebrates history.
func (m *Model) noticeShipped() {
	if m.taskStore == nil {
		return
	}
	all := m.taskStore.List()
	seen := m.lastStatus != nil
	next := make(map[string]tasks.Status, len(all))
	for _, tk := range all {
		next[tk.Slug] = tk.Status
		if !seen || tk.Status != tasks.Done {
			continue
		}
		if prev, ok := m.lastStatus[tk.Slug]; ok && prev != tasks.Done {
			t := shipToast(tk)
			m.toast = &t
		}
	}
	m.lastStatus = next
}

// shipToast names the card and what it took: runs and, when every run
// reported one, the cost (ADR-0011: never guess a price).
func shipToast(tk tasks.Task) kit.Toast {
	detail := ""
	if n := len(tk.Runs); n > 0 {
		detail = kit.Count(n, "run")
		cost, known := 0.0, true
		for _, r := range tk.Runs {
			if !r.HasCost {
				known = false
				break
			}
			cost += r.CostUSD
		}
		if known {
			detail += fmt.Sprintf(" · $%.2f", cost)
		}
	}
	return kit.Toast{Kind: kit.ToastSuccess, Title: tk.Slug + " shipped", Detail: detail}
}

// TakeToast hands the shell the pending toast once (app toaster seam).
func (m *Model) TakeToast() (kit.Toast, bool) {
	m.noticeShipped()
	if m.toast == nil {
		return kit.Toast{}, false
	}
	t := *m.toast
	m.toast = nil
	return t, true
}

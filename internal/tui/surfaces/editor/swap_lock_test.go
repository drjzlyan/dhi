package editor

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/swapfile"
)

// F-161: after a reboot a dead DHI's pid can belong to any process; the swap
// must still be offered for recovery.
func TestSwapReusedPIDStillRecovers(t *testing.T) {
	m, path, now := swapModel(t)
	swapDirty(m, now, "theirs\n")
	st := m.swapStore()
	h, text, _ := st.Read(path)
	if h.Nonce == "" {
		t.Fatal("swap header lacks the instance nonce")
	}
	dead := swapfile.New(st.Dir()) // a DHI that held a lock and is gone
	h.Nonce, h.PID = dead.Nonce(), livePID
	dead.Release()
	m.closeBuffer()
	_ = st.WriteSync(h, text)
	m.open(&node{kind: nodeFile, path: path, name: "app.go"})
	if m.swapCard == nil {
		t.Fatalf("recovery not offered for a reused pid: %q", m.active().Message())
	}
}

// A lock that is still held is a running DHI, whatever its pid says.
func TestSwapHeldLockWarns(t *testing.T) {
	m, path, now := swapModel(t)
	swapDirty(m, now, "theirs\n")
	st := m.swapStore()
	h, text, _ := st.Read(path)
	other := swapfile.New(st.Dir())
	h.Nonce, h.PID = other.Nonce(), deadPID
	m.closeBuffer()
	_ = st.WriteSync(h, text)
	m.open(&node{kind: nodeFile, path: path, name: "app.go"})
	other.Release()
	if m.swapCard != nil || !strings.Contains(m.active().Message(), "open in another DHI") {
		t.Fatalf("card=%v msg=%q", m.swapCard, m.active().Message())
	}
}

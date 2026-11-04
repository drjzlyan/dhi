package editor

import (
	"os"
	"strings"
	"testing"
	"time"
)

const deadPID, livePID, ownPID = 111, 222, 333

// swapModel is an editor with an injected clock and pid liveness.
func swapModel(t *testing.T) (*Model, string, *time.Time) {
	t.Helper()
	m, path := openApp(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	m.pid = ownPID
	m.pidAlive = func(p int) bool { return p == livePID || p == ownPID }
	return m, path, &now
}

func swapDirty(m *Model, now *time.Time, text string) {
	m.active().Buffer().SetText(text)
	m.swapTick()
	*now = now.Add(swapDelay)
	m.swapTick()
	m.swapStore().Wait()
}

// F-161: the swap appears swapDelay after the last change and goes on save.
func TestSwapWrittenDebouncedAndDroppedOnSave(t *testing.T) {
	m, path, now := swapModel(t)
	m.active().Buffer().SetText("edited\n")
	m.swapTick()
	*now = now.Add(swapDelay - time.Millisecond)
	m.swapTick()
	m.swapStore().Wait()
	if _, _, ok := m.swapStore().Read(path); ok {
		t.Fatal("swap written before the delay")
	}
	*now = now.Add(time.Millisecond)
	m.swapTick()
	m.swapStore().Wait()
	h, text, ok := m.swapStore().Read(path)
	if !ok || text != "edited\n" || h.PID != ownPID || h.Hot || !h.At.Equal(*now) || h.BaseHash == "" {
		t.Fatalf("swap = %+v %q %v", h, text, ok)
	}
	if err := m.active().Save(); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := m.swapStore().Read(path); ok {
		t.Fatal("swap survived :w")
	}
}

func TestSwapDroppedOnClose(t *testing.T) {
	m, path, now := swapModel(t)
	swapDirty(m, now, "again\n")
	if _, _, ok := m.swapStore().Read(path); !ok {
		t.Fatal("no swap to drop")
	}
	m.closeBuffer()
	if _, _, ok := m.swapStore().Read(path); ok {
		t.Fatal("swap survived close")
	}
}

// crashed leaves a swap from a dead DHI and reopens the file.
func crashed(t *testing.T, hot bool) (*Model, string) {
	t.Helper()
	m, path, now := swapModel(t)
	swapDirty(m, now, "unsaved work\n")
	st := m.swapStore()
	h, text, _ := st.Read(path)
	h.PID, h.Hot = deadPID, hot
	m.closeBuffer() // removes the swap; put it back as a crash leaves it
	if err := st.WriteSync(h, text); err != nil {
		t.Fatal(err)
	}
	m.open(&node{kind: nodeFile, path: path, name: "app.go"})
	return m, path
}

func TestSwapRecoverIsOneUndoStep(t *testing.T) {
	m, path := crashed(t, false)
	disk, _ := os.ReadFile(path)
	if m.swapCard == nil {
		t.Fatal("no card for a dead DHI's swap")
	}
	if v := plainView(m); !strings.Contains(v, "recover") || strings.Contains(v, "changed on disk") {
		t.Fatalf("card:\n%s", v)
	}
	feed(m, "r")
	e := m.active()
	if m.swapCard != nil || e.Buffer().Text() != "unsaved work\n" || !e.Buffer().Dirty() {
		t.Fatalf("recover: %q dirty=%v", e.Buffer().Text(), e.Buffer().Dirty())
	}
	e.Buffer().Undo()
	if e.Buffer().Text() != string(disk) {
		t.Fatalf("one undo should return the disk text, got %q", e.Buffer().Text())
	}
}

func TestSwapCompareAndDiscard(t *testing.T) {
	m, path := crashed(t, false)
	feed(m, "c")
	if v := plainView(m); !strings.Contains(v, "+unsaved work") || !strings.Contains(v, "file → swap") {
		t.Fatalf("compare:\n%s", v)
	}
	if m.active().Buffer().Dirty() {
		t.Fatal("compare must not touch the buffer")
	}
	feed(m, "esc", "d")
	if m.swapCard != nil || m.active().Buffer().Dirty() {
		t.Fatal("discard changed the buffer or left the card")
	}
	if _, _, ok := m.swapStore().Read(path); ok {
		t.Fatal("discard kept the swap")
	}
}

func TestSwapCardSaysDiskChanged(t *testing.T) {
	m, path, now := swapModel(t)
	swapDirty(m, now, "unsaved work\n")
	st := m.swapStore()
	h, text, _ := st.Read(path)
	h.PID = deadPID
	m.closeBuffer()
	_ = st.WriteSync(h, text)
	if err := os.WriteFile(path, []byte("changed outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.open(&node{kind: nodeFile, path: path, name: "app.go"})
	if m.swapCard == nil || !strings.Contains(plainView(m), "changed on disk since") {
		t.Fatalf("card should name the disk change:\n%s", plainView(m))
	}
}

func TestSwapEscKeepsSwapAndHoldsNewOnes(t *testing.T) {
	m, path := crashed(t, false)
	feed(m, "esc")
	if _, _, ok := m.swapStore().Read(path); !ok || !m.bufs[m.activeTab].noSwap {
		t.Fatal("esc must keep the swap and hold writes")
	}
}

func TestSwapLivePIDWarnsWithoutCard(t *testing.T) {
	m, path, now := swapModel(t)
	swapDirty(m, now, "theirs\n")
	st := m.swapStore()
	h, text, _ := st.Read(path)
	h.PID = livePID
	m.closeBuffer()
	_ = st.WriteSync(h, text)
	m.open(&node{kind: nodeFile, path: path, name: "app.go"})
	if m.swapCard != nil || !strings.Contains(m.active().Message(), "open in another DHI") {
		t.Fatalf("card=%v msg=%q", m.swapCard, m.active().Message())
	}
	swapDirty(m, now, "mine\n")
	if _, got, _ := st.Read(path); got != "theirs\n" {
		t.Fatalf("clobbered the other DHI's swap: %q", got)
	}
}

func TestSwapIdenticalToDiskIsDropped(t *testing.T) {
	m, path, now := swapModel(t)
	swapDirty(m, now, "x\n")
	st := m.swapStore()
	h, _, _ := st.Read(path)
	disk, _ := os.ReadFile(path)
	h.PID = deadPID
	m.closeBuffer()
	_ = st.WriteSync(h, string(disk))
	m.open(&node{kind: nodeFile, path: path, name: "app.go"})
	if _, _, ok := st.Read(path); ok || m.swapCard != nil {
		t.Fatal("a swap equal to the file should vanish quietly")
	}
}

func otherDHI() Option { return WithProcess(999, func(p int) bool { return p == 999 }) }

// Hot exit: keep marks hot; a new editor with session restore on reopens the
// tab with the text and no card.
func TestHotExitRestoresWithoutCard(t *testing.T) {
	m, path, _ := swapModel(t)
	m.active().Buffer().SetText("hot text\n")
	if err := m.KeepForNextTime(); err != nil {
		t.Fatal(err)
	}
	if h, _, _ := m.swapStore().Read(path); !h.Hot {
		t.Fatal("swap not marked hot")
	}
	m.SaveSession()

	m2 := New("test", m.ws, sessionOpts(true), otherDHI())
	if m2.swapCard != nil {
		t.Fatal("hot exit should not show a card")
	}
	tab := m2.tabFor(path)
	if tab == nil || tab.ed.Buffer().Text() != "hot text\n" || !tab.ed.Buffer().Dirty() {
		t.Fatal("hot text not recovered")
	}
}

func TestHotExitWithSessionOffShowsCard(t *testing.T) {
	m, path, _ := swapModel(t)
	m.active().Buffer().SetText("hot text\n")
	if err := m.KeepForNextTime(); err != nil {
		t.Fatal(err)
	}
	m2 := New("test", m.ws, sessionOpts(false), otherDHI())
	m2.open(&node{kind: nodeFile, path: path, name: "app.go"})
	if m2.swapCard == nil {
		t.Fatal("without session restore the swap asks on open")
	}
}

func TestDiscardSwapsOnQuitWithoutSaving(t *testing.T) {
	m, path, now := swapModel(t)
	swapDirty(m, now, "gone\n")
	m.DiscardSwaps()
	if _, _, ok := m.swapStore().Read(path); ok {
		t.Fatal("swap kept after quit without saving")
	}
}

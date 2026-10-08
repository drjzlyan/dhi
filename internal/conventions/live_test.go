package conventions

import (
	"errors"
	"sync"
	"testing"
)

func TestLiveReloadsOnlyWhenTheFingerprintChanges(t *testing.T) {
	fp, loads := "a", 0
	next := Defaults()
	next.Branch.Task = "feat/{slug}"
	l := NewLive(Defaults(), func() string { return fp }, func() (Config, error) { loads++; return next, nil })

	if got := l.Get().Branch.Task; got != Defaults().Branch.Task || loads != 0 {
		t.Fatalf("unchanged files reloaded: %q loads=%d", got, loads)
	}
	fp = "b"
	if got := l.Get().Branch.Task; got != "feat/{slug}" || loads != 1 {
		t.Fatalf("edit not picked up: %q loads=%d", got, loads)
	}
	l.Get()
	l.Get()
	if loads != 1 {
		t.Fatalf("reloaded %d times without another edit", loads)
	}
}

func TestLiveKeepsTheLastGoodRulesWhenAnEditIsBroken(t *testing.T) {
	fp := "a"
	broken := true
	good := Defaults()
	good.Commit.Format = "free"
	l := NewLive(Defaults(), func() string { return fp }, func() (Config, error) {
		if broken {
			return Config{}, errors.New("line 3: bad value")
		}
		return good, nil
	})
	fp = "b"
	if got := l.Get(); got.Commit.Format != Defaults().Commit.Format {
		t.Fatalf("a broken edit replaced the rules: %+v", got.Commit)
	}
	if l.Err() == nil {
		t.Fatal("the failed reload was not reported")
	}
	broken, fp = false, "c"
	if got := l.Get(); got.Commit.Format != "free" || l.Err() != nil {
		t.Fatalf("fixing the file did not recover: %+v err=%v", got.Commit, l.Err())
	}
}

func TestLiveIsSafeUnderConcurrentUse(t *testing.T) {
	var mu sync.Mutex
	n := 0
	l := NewLive(Defaults(), func() string { mu.Lock(); defer mu.Unlock(); return string(rune('a' + n%3)) },
		func() (Config, error) { return Defaults(), nil })
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				mu.Lock()
				n++
				mu.Unlock()
				_ = l.Source()()
			}
		}()
	}
	wg.Wait()
}

func TestStaticNeverChanges(t *testing.T) {
	c := Defaults()
	src := Static(c)
	c.Branch.Task = "mutated-after"
	if src().Branch.Task == "mutated-after" {
		t.Fatal("Static aliases the caller's value")
	}
}

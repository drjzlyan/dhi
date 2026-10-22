package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type memStore struct {
	mu sync.Mutex
	m  map[string]Token
}

func (s *memStore) Get(k string) (Token, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.m[k]
	return t, ok
}
func (s *memStore) Put(k string, t Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = t
	return nil
}
func (s *memStore) Delete(k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, k)
	return nil
}

// fakeAuth is an MCP server and its authorization server in one: the MCP
// endpoint answers 401 with a resource_metadata challenge until it gets
// the current access token; the auth server does DCR (when allowed),
// checks PKCE, and issues/refreshes/revokes tokens.
type fakeAuth struct {
	t          *testing.T
	srv        *httptest.Server
	allowDCR   bool
	mu         sync.Mutex
	challenge  string // code_challenge of the pending authorization
	client     string
	access     string
	refresh    string
	revoked    []string
	refreshes  int
	registered int
}

func newFakeAuth(t *testing.T, allowDCR bool) *fakeAuth {
	f := &fakeAuth{t: t, allowDCR: allowDCR}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		ok := f.access != "" && r.Header.Get("Authorization") == "Bearer "+f.access
		f.mu.Unlock()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+f.srv.URL+`/.well-known/oauth-protected-resource/mcp", scope="read"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"resource": f.srv.URL + "/mcp", "authorization_servers": []string{f.srv.URL + "/auth"}})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server/auth", func(w http.ResponseWriter, r *http.Request) {
		m := map[string]any{"issuer": f.srv.URL + "/auth", "authorization_endpoint": f.srv.URL + "/auth/authorize",
			"token_endpoint": f.srv.URL + "/auth/token", "revocation_endpoint": f.srv.URL + "/auth/revoke",
			"code_challenge_methods_supported": []string{"S256"}}
		if f.allowDCR {
			m["registration_endpoint"] = f.srv.URL + "/auth/register"
		}
		json.NewEncoder(w).Encode(m)
	})
	mux.HandleFunc("/auth/register", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.registered++
		f.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"client_id": "dyn-client"})
	})
	// authorize: the "user" approves at once and the browser follows
	// the redirect (the test's Open does that).
	mux.HandleFunc("/auth/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("code_challenge_method") != "S256" || q.Get("resource") != f.srv.URL+"/mcp" || q.Get("scope") != "read" {
			http.Error(w, "bad request: "+r.URL.RawQuery, 400)
			return
		}
		f.mu.Lock()
		f.challenge, f.client = q.Get("code_challenge"), q.Get("client_id")
		f.mu.Unlock()
		http.Redirect(w, r, q.Get("redirect_uri")+"?code=the-code&state="+url.QueryEscape(q.Get("state")), http.StatusFound)
	})
	mux.HandleFunc("/auth/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge || r.Form.Get("code") != "the-code" || r.Form.Get("client_id") != f.client {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
				return
			}
		case "refresh_token":
			if r.Form.Get("refresh_token") != f.refresh {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
				return
			}
			f.refreshes++
		}
		f.access = fmt.Sprintf("access-%d", f.refreshes)
		f.refresh = fmt.Sprintf("refresh-%d", f.refreshes)
		json.NewEncoder(w).Encode(map[string]any{"access_token": f.access, "refresh_token": f.refresh, "expires_in": 3600})
	})
	mux.HandleFunc("/auth/revoke", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.mu.Lock()
		f.revoked = append(f.revoked, r.Form.Get("token"))
		f.mu.Unlock()
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// browser follows the authorization URL like a user who approves.
func browser(u string) error {
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return nil }}
	go func() {
		resp, err := c.Get(u)
		if err == nil {
			resp.Body.Close()
		}
	}()
	return nil
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestLoginWithDynamicRegistrationThenCall(t *testing.T) {
	f := newFakeAuth(t, true)
	store := &memStore{m: map[string]Token{}}
	var shown string
	o := &OAuth{Store: store, Open: browser, Show: func(_, u string) { shown = u }}
	server := f.srv.URL + "/mcp"
	if need, err := o.NeedsSignIn(context.Background(), server); err != nil || !need {
		t.Fatalf("needs sign-in = %v %v", need, err)
	}
	if _, err := o.Client(context.Background(), server, ClientConfig{}, nil, false); err == nil {
		t.Fatal("no token: Client must ask for a sign-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := o.Login(ctx, server, ClientConfig{}); err != nil {
		t.Fatal(err)
	}
	if f.registered != 1 || f.client != "dyn-client" || !strings.Contains(shown, "code_challenge=") {
		t.Fatalf("registered=%d client=%q shown=%q", f.registered, f.client, shown)
	}
	c, err := o.Client(ctx, server, ClientConfig{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	h, err := DialHTTP(ctx, server, c)
	if err != nil {
		t.Fatalf("dial with token: %v", err)
	}
	h.Close()
}

// A server whose config publishes its client (Slack's plugin) is used
// as is, on its fixed callback port; no registration happens.
func TestPublishedClientIDAndFixedPort(t *testing.T) {
	f := newFakeAuth(t, false)
	o := &OAuth{Store: &memStore{m: map[string]Token{}}, Open: browser}
	port := freePort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var redirect string
	o.Show = func(_, u string) {
		pu, _ := url.Parse(u)
		redirect = pu.Query().Get("redirect_uri")
	}
	if err := o.Login(ctx, f.srv.URL+"/mcp", ClientConfig{ClientID: "slack-app", CallbackPort: port}); err != nil {
		t.Fatal(err)
	}
	if f.client != "slack-app" || f.registered != 0 || redirect != fmt.Sprintf("http://localhost:%d/callback", port) {
		t.Fatalf("client=%q registered=%d redirect=%q", f.client, f.registered, redirect)
	}
}

func TestNoClientAndNoRegistrationIsNamed(t *testing.T) {
	f := newFakeAuth(t, false)
	o := &OAuth{Store: &memStore{m: map[string]Token{}}, Open: browser}
	err := o.Login(context.Background(), f.srv.URL+"/mcp", ClientConfig{})
	if err == nil || !strings.Contains(err.Error(), "does not allow registration") {
		t.Fatalf("err = %v", err)
	}
}

// A 401 with a stale token refreshes once and retries; an expiring token
// refreshes before the call.
func TestRefreshOn401AndBeforeExpiry(t *testing.T) {
	f := newFakeAuth(t, true)
	store := &memStore{m: map[string]Token{}}
	now := time.Now()
	o := &OAuth{Store: store, Open: browser, Now: func() time.Time { return now }}
	server := f.srv.URL + "/mcp"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := o.Login(ctx, server, ClientConfig{}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.access = "rotated-elsewhere" // the server no longer accepts our token
	f.mu.Unlock()
	c, _ := o.Client(ctx, server, ClientConfig{}, nil, false)
	h, err := DialHTTP(ctx, server, c)
	if err != nil {
		t.Fatalf("401 must refresh and retry: %v", err)
	}
	h.Close()
	if f.refreshes != 1 {
		t.Fatalf("refreshes = %d", f.refreshes)
	}
	now = now.Add(2 * time.Hour) // past expiry: refresh before the call
	c, _ = o.Client(ctx, server, ClientConfig{}, nil, false)
	h, err = DialHTTP(ctx, server, c)
	if err != nil || f.refreshes != 2 {
		t.Fatalf("expiry refresh: %v refreshes=%d", err, f.refreshes)
	}
	h.Close()
	if err := o.Logout(ctx, server); err != nil || len(f.revoked) == 0 || o.SignedIn(server) {
		t.Fatalf("logout: %v revoked=%v", err, f.revoked)
	}
}

// Over SSH the browser's redirect cannot reach DHI: pasting the address
// it ended on finishes the sign-in.
func TestPasteBackFinishesTheLogin(t *testing.T) {
	f := newFakeAuth(t, true)
	o := &OAuth{Store: &memStore{m: map[string]Token{}}}
	server := f.srv.URL + "/mcp"
	authURL := make(chan string, 1)
	o.Show = func(_, u string) { authURL <- u }
	done := make(chan error, 1)
	go func() { done <- o.Login(context.Background(), server, ClientConfig{}) }()
	u := <-authURL
	// The user's browser (elsewhere) approves; it ends on the redirect.
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if err := o.PasteRedirect(server, resp.Header.Get("Location")); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil || !o.SignedIn(server) {
		t.Fatalf("paste-back: %v", err)
	}
}

func TestStateMismatchIsRefused(t *testing.T) {
	f := newFakeAuth(t, true)
	o := &OAuth{Store: &memStore{m: map[string]Token{}}}
	server := f.srv.URL + "/mcp"
	urls := make(chan string, 1)
	o.Show = func(_, u string) { urls <- u }
	done := make(chan error, 1)
	go func() { done <- o.Login(context.Background(), server, ClientConfig{}) }()
	<-urls
	o.PasteRedirect(server, "http://localhost:1/callback?code=the-code&state=forged")
	if err := <-done; err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("err = %v", err)
	}
}

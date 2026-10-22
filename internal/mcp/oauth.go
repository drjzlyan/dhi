package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// OAuth signs DHI in to remote MCP servers the way the MCP authorization
// spec describes (ADR-0042, F-106), so a user adds a server and logs in
// with their own account — no app to register, no API key:
//
//  1. A request without a token gets 401; its WWW-Authenticate names the
//     protected-resource metadata (RFC 9728), which names the
//     authorization server, whose metadata (RFC 8414 / OIDC) has the
//     endpoints.
//  2. The client ID is the one the server's own config publishes (Slack's
//     plugin carries one), or DHI registers itself dynamically (RFC 7591)
//     — Atlassian, Linear, Notion and most hosted servers allow that.
//  3. The browser opens the vendor's login page (PKCE S256, a loopback
//     redirect, the resource parameter of RFC 8707); the vendor redirects
//     back with a code DHI exchanges for tokens. Over SSH the user can
//     paste the final address instead.
//  4. Tokens are kept in the secret store, refreshed before they expire
//     and on 401, and revoked on sign-out.
type OAuth struct {
	Store TokenStore
	HTTP  *http.Client
	// Open shows url in the user's browser; Show tells the user the URL
	// (and that a login is waiting) whatever Open did.
	Open func(url string) error
	Show func(server, url string)
	// ClientName is how DHI names itself when it registers.
	ClientName string
	// Now is the clock (tests).
	Now func() time.Time

	mu      sync.Mutex
	pending map[string]*login // server URL → login in progress
}

// TokenStore keeps one record per server (in the secret store).
type TokenStore interface {
	Get(key string) (Token, bool)
	Put(key string, t Token) error
	Delete(key string) error
}

// Token is everything needed to call, refresh and revoke for one server.
type Token struct {
	Access       string    `json:"access_token"`
	Refresh      string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry,omitzero"`
	Scope        string    `json:"scope,omitempty"`
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret,omitempty"`
	TokenURL     string    `json:"token_url"`
	RevokeURL    string    `json:"revoke_url,omitempty"`
	Resource     string    `json:"resource,omitempty"`
}

// ClientConfig is what a server's config says about its OAuth client.
type ClientConfig struct {
	ClientID     string
	CallbackPort int
}

// ErrSignIn reports that a server needs the user to sign in.
var ErrSignIn = errors.New("needs a sign-in")

// SignInError names the server whose login is needed.
type SignInError struct{ Server string }

func (e SignInError) Error() string {
	return e.Server + " needs a sign-in: Settings → INTEGRATIONS → enter (or wait for the browser)"
}

func (e SignInError) Unwrap() error { return ErrSignIn }

func (o *OAuth) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o *OAuth) http() *http.Client {
	if o.HTTP != nil {
		return o.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Key is the store key for a server URL.
func Key(serverURL string) string { return canonicalResource(serverURL) }

// canonicalResource is the server URL as RFC 8707 wants it: lowercase
// scheme and host, no fragment.
func canonicalResource(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Scheme, u.Host, u.Fragment = strings.ToLower(u.Scheme), strings.ToLower(u.Host), ""
	return u.String()
}

// Client returns an HTTP client that sends the server's token, refreshing
// it when needed. Without a token it returns a SignInError (and, when
// background is set, starts a sign-in so the next attempt succeeds).
func (o *OAuth) Client(ctx context.Context, serverURL string, cfg ClientConfig, base *http.Client, background bool) (*http.Client, error) {
	key := Key(serverURL)
	tok, ok := o.Store.Get(key)
	if !ok {
		if background {
			go func() {
				lctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				_ = o.Login(lctx, serverURL, cfg)
			}()
		}
		return nil, SignInError{Server: hostOf(serverURL)}
	}
	if base == nil {
		base = &http.Client{}
	}
	rt := base.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}
	c := *base
	c.Transport = &oauthTransport{o: o, key: key, tok: tok, next: rt}
	return &c, nil
}

// NeedsSignIn probes a server: true when it answers 401 with a Bearer
// challenge (it uses OAuth) and no token is stored.
func (o *OAuth) NeedsSignIn(ctx context.Context, serverURL string) (bool, error) {
	if _, ok := o.Store.Get(Key(serverURL)); ok {
		return false, nil
	}
	_, status, err := o.probe(ctx, serverURL)
	if err != nil {
		return false, err
	}
	return status == http.StatusUnauthorized, nil
}

type oauthTransport struct {
	o    *OAuth
	key  string
	next http.RoundTripper
	mu   sync.Mutex
	tok  Token
}

func (t *oauthTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.mu.Lock()
	if !t.tok.Expiry.IsZero() && t.o.now().After(t.tok.Expiry.Add(-time.Minute)) && t.tok.Refresh != "" {
		if nt, err := t.o.refresh(r.Context(), t.tok); err == nil {
			t.tok = nt
			_ = t.o.Store.Put(t.key, nt)
		}
	}
	tok := t.tok
	t.mu.Unlock()
	body, err := snapshotBody(r)
	if err != nil {
		return nil, err
	}
	resp, err := t.send(r, body, tok.Access)
	if err != nil || resp.StatusCode != http.StatusUnauthorized || tok.Refresh == "" {
		return resp, err
	}
	// 401 with a refresh token: refresh once and retry.
	_ = resp.Body.Close()
	nt, rerr := t.o.refresh(r.Context(), tok)
	if rerr != nil {
		_ = t.o.Store.Delete(t.key) // the grant is gone: sign in again
		return nil, SignInError{Server: hostOf(t.key)}
	}
	t.mu.Lock()
	t.tok = nt
	t.mu.Unlock()
	_ = t.o.Store.Put(t.key, nt)
	return t.send(r, body, nt.Access)
}

func (t *oauthTransport) send(r *http.Request, body []byte, access string) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	if body != nil {
		r2.Body = io.NopCloser(bytes.NewReader(body))
	}
	r2.Header.Set("Authorization", "Bearer "+access)
	return t.next.RoundTrip(r2)
}

func snapshotBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	b, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	return b, err
}

// ---- discovery ----

type resourceMeta struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

type authServerMeta struct {
	Issuer                 string   `json:"issuer"`
	AuthorizationEndpoint  string   `json:"authorization_endpoint"`
	TokenEndpoint          string   `json:"token_endpoint"`
	RegistrationEndpoint   string   `json:"registration_endpoint"`
	RevocationEndpoint     string   `json:"revocation_endpoint"`
	ScopesSupported        []string `json:"scopes_supported"`
	CodeChallengeMethods   []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMeths []string `json:"token_endpoint_auth_methods_supported"`
}

// probe sends an unauthenticated initialize and returns the challenge.
func (o *OAuth) probe(ctx context.Context, serverURL string) (http.Header, int, error) {
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"dhi","version":"1"}}}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL, strings.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := o.http().Do(req)
	if err != nil {
		return nil, 0, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
	return resp.Header, resp.StatusCode, nil
}

// challengeParam reads a parameter of the Bearer challenge.
func challengeParam(h http.Header, name string) string {
	for _, v := range h.Values("WWW-Authenticate") {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "Bearer"))
			k, val, ok := strings.Cut(strings.TrimSpace(part), "=")
			if ok && strings.EqualFold(strings.TrimSpace(k), name) {
				return strings.Trim(strings.TrimSpace(val), `"`)
			}
		}
	}
	return ""
}

func (o *OAuth) getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := o.http().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", u, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

// discover finds the authorization server for serverURL.
func (o *OAuth) discover(ctx context.Context, serverURL string) (*authServerMeta, string, error) {
	h, status, err := o.probe(ctx, serverURL)
	if err != nil {
		return nil, "", err
	}
	if status != http.StatusUnauthorized {
		return nil, "", fmt.Errorf("%s did not ask for a sign-in (HTTP %d)", hostOf(serverURL), status)
	}
	scope := challengeParam(h, "scope")
	u, _ := url.Parse(serverURL)
	origin := u.Scheme + "://" + u.Host
	var rm resourceMeta
	metaURL := challengeParam(h, "resource_metadata")
	candidates := []string{metaURL,
		origin + "/.well-known/oauth-protected-resource" + strings.TrimRight(u.Path, "/"),
		origin + "/.well-known/oauth-protected-resource"}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if err := o.getJSON(ctx, c, &rm); err == nil && len(rm.AuthorizationServers) > 0 {
			break
		}
	}
	issuer := origin // older servers: the server's origin is its own authorization server
	if len(rm.AuthorizationServers) > 0 {
		issuer = strings.TrimRight(rm.AuthorizationServers[0], "/")
	}
	if scope == "" && len(rm.ScopesSupported) > 0 {
		scope = strings.Join(rm.ScopesSupported, " ")
	}
	iu, err := url.Parse(issuer)
	if err != nil {
		return nil, "", err
	}
	path := strings.TrimRight(iu.Path, "/")
	base := iu.Scheme + "://" + iu.Host
	var as authServerMeta
	for _, c := range []string{
		base + "/.well-known/oauth-authorization-server" + path,
		base + "/.well-known/openid-configuration" + path,
		issuer + "/.well-known/openid-configuration",
	} {
		if err := o.getJSON(ctx, c, &as); err == nil && as.TokenEndpoint != "" {
			return &as, scope, nil
		}
	}
	// The 2025-03 spec's last resort: default endpoints on the issuer.
	return &authServerMeta{AuthorizationEndpoint: base + "/authorize", TokenEndpoint: base + "/token",
		RegistrationEndpoint: base + "/register"}, scope, nil
}

// ---- login ----

type login struct {
	done  chan struct{}
	err   error
	paste chan string
	url   string
}

// Pending reports a login waiting in the browser for serverURL, and its
// authorization URL (to show or copy).
func (o *OAuth) Pending(serverURL string) (string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	l, ok := o.pending[Key(serverURL)]
	if !ok {
		return "", false
	}
	return l.url, true
}

// PasteRedirect finishes a waiting login with the address the browser
// ended on (for SSH sessions, where the loopback redirect cannot land).
func (o *OAuth) PasteRedirect(serverURL, redirected string) error {
	o.mu.Lock()
	l, ok := o.pending[Key(serverURL)]
	o.mu.Unlock()
	if !ok {
		return fmt.Errorf("no sign-in is waiting for %s", hostOf(serverURL))
	}
	select {
	case l.paste <- redirected:
		return nil
	default:
		return fmt.Errorf("the sign-in already finished")
	}
}

// Login runs the browser sign-in for serverURL and stores the tokens.
// Concurrent calls for one server share a single login.
func (o *OAuth) Login(ctx context.Context, serverURL string, cfg ClientConfig) error {
	key := Key(serverURL)
	o.mu.Lock()
	if o.pending == nil {
		o.pending = map[string]*login{}
	}
	if l, ok := o.pending[key]; ok {
		o.mu.Unlock()
		select {
		case <-l.done:
			return l.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	l := &login{done: make(chan struct{}), paste: make(chan string, 1)}
	o.pending[key] = l
	o.mu.Unlock()
	l.err = o.login(ctx, serverURL, cfg, l)
	o.mu.Lock()
	delete(o.pending, key)
	o.mu.Unlock()
	close(l.done)
	return l.err
}

func (o *OAuth) login(ctx context.Context, serverURL string, cfg ClientConfig, l *login) error {
	as, scope, err := o.discover(ctx, serverURL)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.CallbackPort))
	if err != nil {
		return fmt.Errorf("listen for the sign-in redirect on port %d: %w", cfg.CallbackPort, err)
	}
	defer func() { _ = ln.Close() }()
	redirect := fmt.Sprintf("http://localhost:%d/callback", ln.Addr().(*net.TCPAddr).Port)

	clientID, secret := cfg.ClientID, ""
	if clientID == "" {
		if as.RegistrationEndpoint == "" {
			return fmt.Errorf("%s needs an OAuth app this config does not name, and it does not allow registration", hostOf(serverURL))
		}
		clientID, secret, err = o.register(ctx, as.RegistrationEndpoint, redirect)
		if err != nil {
			return err
		}
	}
	verifier := randomString(48)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	state := randomString(24)
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirect},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"resource":              {canonicalResource(serverURL)},
	}
	if scope != "" {
		q.Set("scope", scope)
	}
	authURL := as.AuthorizationEndpoint
	if strings.Contains(authURL, "?") {
		authURL += "&" + q.Encode()
	} else {
		authURL += "?" + q.Encode()
	}
	o.mu.Lock()
	l.url = authURL
	o.mu.Unlock()

	codes := make(chan url.Values, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, "<!doctype html><title>DHI</title><p style=\"font-family:sans-serif\">Signed in. You can close this tab and return to DHI.</p>")
		select {
		case codes <- r.URL.Query():
		default:
		}
	})}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	if o.Show != nil {
		o.Show(hostOf(serverURL), authURL)
	}
	if o.Open != nil {
		_ = o.Open(authURL)
	}
	var got url.Values
	select {
	case got = <-codes:
	case pasted := <-l.paste:
		u, err := url.Parse(strings.TrimSpace(pasted))
		if err != nil {
			return fmt.Errorf("that is not the address the browser ended on: %w", err)
		}
		got = u.Query()
	case <-ctx.Done():
		return fmt.Errorf("sign-in to %s timed out", hostOf(serverURL))
	}
	if e := got.Get("error"); e != "" {
		return fmt.Errorf("%s refused the sign-in: %s %s", hostOf(serverURL), e, got.Get("error_description"))
	}
	if got.Get("state") != state {
		return fmt.Errorf("the sign-in response does not match this request (state); try again")
	}
	tok, err := o.exchange(ctx, as, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {got.Get("code")},
		"redirect_uri":  {redirect},
		"code_verifier": {verifier},
		"resource":      {canonicalResource(serverURL)},
	}, clientID, secret)
	if err != nil {
		return err
	}
	tok.RevokeURL, tok.Resource = as.RevocationEndpoint, canonicalResource(serverURL)
	return o.Store.Put(Key(serverURL), tok)
}

// register is dynamic client registration (RFC 7591).
func (o *OAuth) register(ctx context.Context, endpoint, redirect string) (string, string, error) {
	name := o.ClientName
	if name == "" {
		name = "DHI"
	}
	body, _ := json.Marshal(map[string]any{
		"client_name":                name,
		"redirect_uris":              []string{redirect},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.http().Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", "", fmt.Errorf("register with %s: %s %s", hostOf(endpoint), resp.Status, strings.TrimSpace(string(b)))
	}
	var out struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.ClientID == "" {
		return "", "", fmt.Errorf("register with %s: no client_id in the answer", hostOf(endpoint))
	}
	return out.ClientID, out.ClientSecret, nil
}

func (o *OAuth) exchange(ctx context.Context, as *authServerMeta, form url.Values, clientID, secret string) (Token, error) {
	form.Set("client_id", clientID)
	if secret != "" {
		form.Set("client_secret", secret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, as.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := o.http().Do(req)
	if err != nil {
		return Token{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
		OK           *bool  `json:"ok"` // Slack wraps answers in {"ok":…}
		Authed       *struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			ExpiresIn    int    `json:"expires_in"`
		} `json:"authed_user"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return Token{}, fmt.Errorf("token answer from %s: %w", hostOf(as.TokenEndpoint), err)
	}
	if out.AccessToken == "" && out.Authed != nil { // Slack's user-token shape
		out.AccessToken, out.RefreshToken, out.ExpiresIn = out.Authed.AccessToken, out.Authed.RefreshToken, out.Authed.ExpiresIn
	}
	if resp.StatusCode/100 != 2 || out.AccessToken == "" {
		msg := out.Error
		if out.ErrorDesc != "" {
			msg += ": " + out.ErrorDesc
		}
		if msg == "" {
			msg = resp.Status
		}
		return Token{}, fmt.Errorf("%s did not issue a token: %s", hostOf(as.TokenEndpoint), msg)
	}
	t := Token{Access: out.AccessToken, Refresh: out.RefreshToken, Scope: out.Scope,
		ClientID: clientID, ClientSecret: secret, TokenURL: as.TokenEndpoint}
	if out.ExpiresIn > 0 {
		t.Expiry = o.now().Add(time.Duration(out.ExpiresIn) * time.Second)
	}
	return t, nil
}

func (o *OAuth) refresh(ctx context.Context, t Token) (Token, error) {
	as := &authServerMeta{TokenEndpoint: t.TokenURL}
	nt, err := o.exchange(ctx, as, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {t.Refresh},
		"resource": {t.Resource}}, t.ClientID, t.ClientSecret)
	if err != nil {
		return Token{}, err
	}
	if nt.Refresh == "" {
		nt.Refresh = t.Refresh // servers may keep the refresh token
	}
	nt.RevokeURL, nt.Resource = t.RevokeURL, t.Resource
	return nt, nil
}

// Logout revokes the server's tokens (when the server offers revocation)
// and forgets them.
func (o *OAuth) Logout(ctx context.Context, serverURL string) error {
	key := Key(serverURL)
	t, ok := o.Store.Get(key)
	if !ok {
		return nil
	}
	if t.RevokeURL != "" {
		for _, tok := range []string{t.Refresh, t.Access} {
			if tok == "" {
				continue
			}
			form := url.Values{"token": {tok}, "client_id": {t.ClientID}}
			if req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.RevokeURL, strings.NewReader(form.Encode())); err == nil {
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if resp, err := o.http().Do(req); err == nil {
					_ = resp.Body.Close()
				}
			}
		}
	}
	return o.Store.Delete(key)
}

// SignedIn reports whether a token is stored for serverURL.
func (o *OAuth) SignedIn(serverURL string) bool {
	_, ok := o.Store.Get(Key(serverURL))
	return ok
}

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:n]
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

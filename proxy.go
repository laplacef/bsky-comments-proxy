package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxUpstreamBody = 4 << 20

type upstream struct {
	base   string
	client *http.Client
}

func newUpstream(base string) *upstream {
	return &upstream{
		base:   strings.TrimRight(base, "/"),
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// upstreamError carries a non-200 upstream reply through the cache layer
// so handlers can forward the status without caching it.
type upstreamError struct {
	status int
	body   []byte
}

func (e *upstreamError) Error() string {
	return fmt.Sprintf("upstream responded %d", e.status)
}

func (u *upstream) get(path string, params url.Values, accept string) ([]byte, string, error) {
	target := u.base + path
	if len(params) > 0 {
		target += "?" + params.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", accept)
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamBody))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", &upstreamError{status: resp.StatusCode, body: body}
	}
	return body, resp.Header.Get("Content-Type"), nil
}

type server struct {
	up        *upstream
	cdn       *upstream
	threads   *cache
	avatars   *cache
	actors    map[string]bool
	publicURL string
}

// allowed gates thread reads to configured handles/DIDs, so the proxy is
// not an open relay. An empty allowlist permits any actor.
func (s *server) allowed(actor string) bool {
	return len(s.actors) == 0 || s.actors[strings.ToLower(actor)]
}

var (
	// Handles are domain names; DIDs stay close to the did:method:id grammar.
	handleRe  = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
	postURIRe = regexp.MustCompile(`^at://(did:[a-z]+:[A-Za-z0-9._:%-]+|[A-Za-z0-9.-]+)/app\.bsky\.feed\.post/([A-Za-z0-9._~:-]{1,512})$`)
)

func writeJSONError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

// writeUpstreamError reports whether err was handled. Bluesky's own JSON
// errors pass through; anything else becomes an opaque 502.
func writeUpstreamError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	var ue *upstreamError
	if errors.As(err, &ue) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(ue.status)
		_, _ = w.Write(ue.body)
		return true
	}
	writeJSONError(w, http.StatusBadGateway, "UpstreamUnreachable")
	return true
}

func writeJSON(w http.ResponseWriter, ttl time.Duration, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", int(ttl.Seconds())))
	_, _ = w.Write(body)
}

func (s *server) resolveHandle(w http.ResponseWriter, r *http.Request) {
	handle := r.URL.Query().Get("handle")
	if !handleRe.MatchString(handle) || !strings.Contains(handle, ".") {
		writeJSONError(w, http.StatusBadRequest, "InvalidRequest")
		return
	}
	if !s.allowed(handle) {
		writeJSONError(w, http.StatusForbidden, "Forbidden")
		return
	}
	key := "handle\x00" + strings.ToLower(handle)
	body, _, err := s.threads.get(key, func() ([]byte, string, error) {
		return s.up.get("/xrpc/com.atproto.identity.resolveHandle",
			url.Values{"handle": {handle}}, "application/json")
	})
	if writeUpstreamError(w, err) {
		return
	}
	writeJSON(w, s.threads.ttl, body)
}

func (s *server) getPostThread(w http.ResponseWriter, r *http.Request) {
	uri := r.URL.Query().Get("uri")
	m := postURIRe.FindStringSubmatch(uri)
	if m == nil {
		writeJSONError(w, http.StatusBadRequest, "InvalidRequest")
		return
	}
	if !s.allowed(m[1]) {
		writeJSONError(w, http.StatusForbidden, "Forbidden")
		return
	}
	depth := 6
	if d := r.URL.Query().Get("depth"); d != "" {
		n, err := strconv.Atoi(d)
		if err != nil || n < 0 || n > 1000 {
			writeJSONError(w, http.StatusBadRequest, "InvalidRequest")
			return
		}
		depth = n
	}
	key := "thread\x00" + uri + "\x00" + strconv.Itoa(depth)
	body, _, err := s.threads.get(key, func() ([]byte, string, error) {
		return s.up.get("/xrpc/app.bsky.feed.getPostThread", url.Values{
			"uri":   {uri},
			"depth": {strconv.Itoa(depth)},
		}, "application/json")
	})
	if writeUpstreamError(w, err) {
		return
	}
	writeJSON(w, s.threads.ttl, s.rewriteAvatars(r, body))
}

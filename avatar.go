package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
)

// Mirrors of CDN avatar paths: img/avatar/plain/<did>/<cid>, with an
// optional @<format> suffix or _thumbnail variant. Nothing else on the
// CDN is reachable.
var avatarPathRe = regexp.MustCompile(`^img/avatar(?:_thumbnail)?/plain/[A-Za-z0-9._:%-]+/[A-Za-z0-9]+(?:@[a-z0-9]+)?$`)

func (s *server) avatar(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/avatar/")
	if !avatarPathRe.MatchString(rest) {
		writeJSONError(w, http.StatusNotFound, "NotFound")
		return
	}
	body, ctype, err := s.avatars.get(rest, func() ([]byte, string, error) {
		return s.cdn.get("/"+rest, nil, "image/*")
	})
	if err != nil {
		var ue *upstreamError
		if errors.As(err, &ue) {
			// Don't echo CDN error bodies; the status is enough.
			writeJSONError(w, ue.status, "UpstreamError")
			return
		}
		writeJSONError(w, http.StatusBadGateway, "UpstreamUnreachable")
		return
	}
	if !strings.HasPrefix(ctype, "image/") {
		writeJSONError(w, http.StatusBadGateway, "UpstreamError")
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(body)
}

// publicBase is the absolute URL prefix readers should use for mirrored
// avatars: the -public-url flag when set, otherwise derived per request.
func (s *server) publicBase(r *http.Request) string {
	if s.publicURL != "" {
		return s.publicURL
	}
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme != "" {
		scheme = strings.TrimSpace(strings.SplitN(scheme, ",", 2)[0])
	} else if r.TLS != nil {
		scheme = "https"
	} else {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

// rewriteAvatars points every author avatar at this proxy. The raw upstream
// body stays in cache, so rewriting happens per response.
func (s *server) rewriteAvatars(r *http.Request, body []byte) []byte {
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return body
	}
	rewriteAvatarValues(doc, s.cdn.base+"/", s.publicBase(r)+"/avatar/")
	out, err := json.Marshal(doc)
	if err != nil {
		return body
	}
	return out
}

func rewriteAvatarValues(v any, cdnPrefix, proxyPrefix string) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if k == "avatar" {
				if s, ok := val.(string); ok && strings.HasPrefix(s, cdnPrefix) {
					t[k] = proxyPrefix + s[len(cdnPrefix):]
				}
				continue
			}
			rewriteAvatarValues(val, cdnPrefix, proxyPrefix)
		}
	case []any:
		for _, item := range t {
			rewriteAvatarValues(item, cdnPrefix, proxyPrefix)
		}
	}
}

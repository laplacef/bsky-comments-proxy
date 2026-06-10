package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Cloud platforms hand the port over as $PORT; fall back to 8080 locally.
func defaultAddr() string {
	if port := os.Getenv("PORT"); port != "" {
		return ":" + port
	}
	return ":8080"
}

func main() {
	addr := flag.String("addr", defaultAddr(), "listen address")
	upstreamBase := flag.String("upstream", "https://public.api.bsky.app", "bluesky XRPC base URL")
	cdnBase := flag.String("cdn", "https://cdn.bsky.app", "bluesky CDN base URL for avatars")
	ttl := flag.Duration("ttl", 45*time.Second, "thread and handle cache TTL")
	avatarTTL := flag.Duration("avatar-ttl", time.Hour, "avatar image cache TTL")
	publicURL := flag.String("public-url", "", "external base URL for rewritten avatar links (defaults to the request host)")
	flag.Parse()

	s := &server{
		up:        newUpstream(*upstreamBase),
		cdn:       newUpstream(*cdnBase),
		threads:   newCache(*ttl, 1024),
		avatars:   newCache(*avatarTTL, 512),
		publicURL: strings.TrimRight(*publicURL, "/"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /xrpc/com.atproto.identity.resolveHandle", s.resolveHandle)
	mux.HandleFunc("GET /xrpc/app.bsky.feed.getPostThread", s.getPostThread)
	mux.HandleFunc("GET /avatar/", s.avatar)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	go func() {
		log.Printf("listening on %s", *addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

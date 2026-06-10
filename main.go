package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	ttl := flag.Duration("ttl", 45*time.Second, "thread and handle cache TTL")
	flag.Parse()

	s := &server{
		up:      newUpstream(*upstreamBase),
		threads: newCache(*ttl, 1024),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /xrpc/com.atproto.identity.resolveHandle", s.resolveHandle)
	mux.HandleFunc("GET /xrpc/app.bsky.feed.getPostThread", s.getPostThread)

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

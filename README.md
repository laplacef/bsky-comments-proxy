# bsky-comments-proxy

A small caching reverse proxy for the [Bluesky](https://bsky.app) read endpoints
that comment widgets use. Put it between your readers and Bluesky and the
browser only ever talks to your origin: thread fetches, handle resolution, and
avatar images all go through the proxy, so a reader's IP address never reaches
Bluesky.

It pairs with
[`bsky-comments-client`](https://github.com/laplacef/bsky-comments-client), a
web component that accepts any base URL through its `endpoint` attribute, but it
works for any client that speaks the same two XRPC calls.

## Why

Embedding Bluesky comments usually means every reader's browser queries
`public.api.bsky.app` and loads avatars from `cdn.bsky.app`, disclosing each
reader's IP to Bluesky. This proxy moves those requests server-side:

- **Privacy.** Readers connect to your origin only. The proxy's egress IP is
  the single address Bluesky sees.
- **Caching.** Responses are cached with a short TTL, so a busy page costs one
  upstream call per TTL window instead of one per reader.
- **Containment.** A strict consuming-site CSP shrinks to
  `connect-src` and `img-src` on your own origins.

The binary is a single static executable built from the Go standard library,
with no runtime dependencies.

## Endpoints

The proxy mirrors the two XRPC reads with the same parameters and response
shapes as Bluesky, plus an avatar mirror:

| Route | Mirrors | Notes |
|-------|---------|-------|
| `/xrpc/com.atproto.identity.resolveHandle?handle=…` | Bluesky XRPC | Cached for `-ttl` |
| `/xrpc/app.bsky.feed.getPostThread?uri=…&depth=…` | Bluesky XRPC | Cached for `-ttl`; avatar URLs rewritten to `/avatar/…` |
| `/avatar/…` | `cdn.bsky.app` avatar images | Cached for `-avatar-ttl`; avatar paths only |
| `/healthz` | (none) | Liveness probe, returns 204 |

Because thread responses rewrite `author.avatar` to the proxy itself, the
browser never contacts the Bluesky CDN either.

## Run

```sh
go build .
./bsky-comments-proxy -origins https://example.com -actors you.bsky.social,did:plc:yourdid
```

Then point the component at it:

```html
<bsky-comments post="..." endpoint="https://comments.example.com"></bsky-comments>
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-addr` | `:$PORT` or `:8080` | Listen address |
| `-upstream` | `https://public.api.bsky.app` | Bluesky XRPC base URL |
| `-cdn` | `https://cdn.bsky.app` | Bluesky CDN base URL for avatars |
| `-ttl` | `45s` | Thread and handle cache TTL |
| `-avatar-ttl` | `1h` | Avatar image cache TTL |
| `-origins` | (none) | Comma-separated origins allowed for cross-origin reads |
| `-actors` | (none) | Comma-separated handles/DIDs whose posts may be fetched |
| `-public-url` | request host | External base URL used in rewritten avatar links |

## Access control

Two allowlists keep the proxy from becoming an open relay:

- **Origins.** CORS headers are only emitted for origins named in `-origins`.
  With the flag unset, no cross-origin reads are possible at all, so set it to
  the site(s) embedding the component.
- **Actors.** When `-actors` is set, thread and handle lookups are restricted
  to the listed handles and DIDs. List both the handle and its DID: clients
  resolve the handle first, then query the thread by DID.

> [!NOTE]
> CORS protects browser reads, not server-to-server requests. If the proxy is
> reachable from the open internet and you want it locked down further, add
> rate limiting at the edge (a CDN or load balancer in front works well).

## Caching

The thread cache collapses concurrent misses into a single upstream call, so a
traffic spike on one post produces one Bluesky request per TTL window.
Responses carry `Cache-Control: public, max-age=<ttl>`, which lets an edge
cache in front of the proxy absorb most traffic before it reaches the binary.

Comment threads tolerate short staleness well; the default 45 seconds keeps
them feeling live while staying polite to the upstream API.

## Privacy

- The proxy adds no cookies, no tracking, and no request logging of reader
  addresses.
- Upstream requests carry only the query parameters Bluesky needs; reader
  headers are not forwarded.
- Avatar mirroring keeps image loads on your origin. The consuming page's CSP
  can be as tight as:

```
connect-src https://comments.example.com;
img-src 'self' https://comments.example.com;
```

## Container

A multi-stage build produces a distroless image that runs as a non-root user:

```sh
docker build -t bsky-comments-proxy .
docker run -p 8080:8080 bsky-comments-proxy -origins https://example.com
```

The server honors `$PORT` and shuts down gracefully on `SIGTERM`, so it fits
scale-to-zero platforms (Cloud Run and similar) without extra configuration.

## Building from source

Go 1.26 or later is the only requirement:

```sh
go build ./...
go vet ./...
```

## License

Apache-2.0, © 2026 Francisco Laplace. See [LICENSE](./LICENSE).

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bsky-comments-proxy .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /bsky-comments-proxy /bsky-comments-proxy
EXPOSE 8080
ENTRYPOINT ["/bsky-comments-proxy"]

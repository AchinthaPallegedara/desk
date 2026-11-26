# Claviq Desk backend: REST API + MCP + OAuth, with the Typst CLI for PDFs.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# Typst is a single static binary (musl build): no headless browser, no extra service.
FROM alpine:3.22 AS typst
ARG TYPST_VERSION=0.15.1
RUN apk add --no-cache curl xz \
 && curl -fsSL "https://github.com/typst/typst/releases/download/v${TYPST_VERSION}/typst-x86_64-unknown-linux-musl.tar.xz" \
  | tar -xJ --strip-components=1 -C /usr/local/bin typst-x86_64-unknown-linux-musl/typst \
 && typst --version

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -H -u 10001 app \
 && mkdir -p /data && chown app:app /data
COPY --from=typst /usr/local/bin/typst /usr/local/bin/typst
COPY --from=build /out/server /usr/local/bin/server
USER app
ENV APP_ENV=production HTTP_ADDR=:8080 DATA_DIR=/data TYPST_BIN=/usr/local/bin/typst
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s \
  CMD wget -qO- http://localhost:8080/api/health >/dev/null || exit 1
ENTRYPOINT ["server"]

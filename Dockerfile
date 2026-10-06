# syntax=docker/dockerfile:1

# ---- build ----
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---- runtime ----
FROM alpine:3.24
# ca-certificates: the server calls DashScope and Supabase (JWKS) over HTTPS.
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 app \
    && mkdir -p /data/uploads \
    && chown app /data/uploads
USER app
COPY --from=build /out/server /usr/local/bin/server

# Mount a persistent volume at /data if uploads must survive restarts.
ENV UPLOAD_DIR=/data/uploads \
    SERVER_PORT=8080
EXPOSE 8080
# Liveness probe for platforms: GET /healthz (no auth).
ENTRYPOINT ["server"]

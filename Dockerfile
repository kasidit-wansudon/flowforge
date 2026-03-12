# ==============================================================================
# Stage 1: Go builder — compile all Go binaries
# ==============================================================================
FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS go-builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=0
ENV GOOS=${TARGETOS}
ENV GOARCH=${TARGETARCH}

RUN go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
    -o /out/flowforge-server \
    ./cmd/server

RUN go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
    -o /out/flowforge-worker \
    ./cmd/worker

RUN go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
    -o /out/flowforge-cli \
    ./cmd/cli

RUN go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
    -o /out/flowforge-migrate \
    ./cmd/migrate

# ==============================================================================
# Stage 2: Frontend builder — build the React UI
# ==============================================================================
FROM --platform=$BUILDPLATFORM node:20-alpine AS frontend-builder

WORKDIR /app

COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci --prefer-offline --no-audit

COPY frontend/ .
RUN npm run build

# ==============================================================================
# Stage 3: Server image — API server + frontend static assets
# ==============================================================================
FROM alpine:3.20 AS server

RUN apk add --no-cache ca-certificates tzdata curl && \
    addgroup -g 1000 flowforge && \
    adduser -u 1000 -G flowforge -s /bin/sh -D flowforge

COPY --from=go-builder /out/flowforge-server /usr/local/bin/flowforge-server
COPY --from=go-builder /out/flowforge-migrate /usr/local/bin/flowforge-migrate
COPY --from=frontend-builder /app/dist /srv/frontend

COPY migrations/ /etc/flowforge/migrations/

RUN chmod +x /usr/local/bin/flowforge-server /usr/local/bin/flowforge-migrate

USER flowforge
WORKDIR /home/flowforge

ENV FLOWFORGE_FRONTEND_DIR=/srv/frontend
ENV FLOWFORGE_MIGRATIONS_DIR=/etc/flowforge/migrations

EXPOSE 8080 9090 6060

HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -sf http://localhost:8080/healthz || exit 1

ENTRYPOINT ["flowforge-server"]
CMD ["--config=/etc/flowforge/config.yaml"]

# ==============================================================================
# Stage 4: Worker image — task execution engine
# ==============================================================================
FROM alpine:3.20 AS worker

RUN apk add --no-cache ca-certificates tzdata curl bash git docker-cli && \
    addgroup -g 1000 flowforge && \
    adduser -u 1000 -G flowforge -s /bin/sh -D flowforge

COPY --from=go-builder /out/flowforge-worker /usr/local/bin/flowforge-worker

RUN chmod +x /usr/local/bin/flowforge-worker

USER flowforge
WORKDIR /home/flowforge

EXPOSE 9091

HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -sf http://localhost:9091/healthz || exit 1

ENTRYPOINT ["flowforge-worker"]
CMD ["--config=/etc/flowforge/config.yaml"]

# ==============================================================================
# Stage 5: CLI image — command-line interface
# ==============================================================================
FROM alpine:3.20 AS cli

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -g 1000 flowforge && \
    adduser -u 1000 -G flowforge -s /bin/sh -D flowforge

COPY --from=go-builder /out/flowforge-cli /usr/local/bin/flowforge

RUN chmod +x /usr/local/bin/flowforge

USER flowforge
WORKDIR /home/flowforge

ENTRYPOINT ["flowforge"]

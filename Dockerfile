# Frontend build stage: compile the SvelteKit SPA.
FROM node:22-alpine AS web
WORKDIR /web
RUN npm install -g pnpm
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# Build stage: compile the Go binary with the embedded frontend.
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Copy go module files and download dependencies.
COPY go.mod go.sum ./
RUN go mod download

# Copy source code, then the compiled frontend build.
COPY . .
COPY --from=web /web/build ./web/build

# Build the binary.
RUN CGO_ENABLED=0 go build -ldflags "-X github.com/barats/xlistman/cmd.Version=$(git describe --tags --always 2>/dev/null || echo dev)" -o xlistman .

# Runtime stage: minimal image
FROM scratch

# CA certificates so outbound STARTTLS (net/smtp.SendMail) can verify real
# relays; scratch has no system root pool.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

# Copy the binary and default config.
COPY --from=builder /build/xlistman /xlistman
COPY config.default.yaml /etc/xlistman/config.yaml

# Point the daemon at the baked-in config; env overrides below win.
ENV XLISTMAN_CONFIG=/etc/xlistman/config.yaml

# Expose ports (HTTP and LMTP).
EXPOSE 8080 8024

# Volume for SQLite database.
VOLUME ["/data"]

# Set environment defaults.
ENV XLISTMAN_DATABASE_PATH=/data/xlistman.db

ENTRYPOINT ["/xlistman"]
CMD ["serve"]

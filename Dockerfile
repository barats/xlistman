# Build stage: compile the Go binary
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Copy go module files and download dependencies.
COPY go.mod go.sum ./
RUN GOPROXY=https://goproxy.cn,direct go mod download

# Copy source code.
COPY . .

# Build the binary.
RUN CGO_ENABLED=0 go build -ldflags "-X github.com/barat/xlistman/cmd.Version=$(git describe --tags --always 2>/dev/null || echo dev)" -o xlistman .

# Runtime stage: minimal image
FROM scratch

# Copy the binary and default config.
COPY --from=builder /build/xlistman /xlistman

# Expose ports (HTTP and LMTP).
EXPOSE 8080 8024

# Volume for SQLite database.
VOLUME ["/data"]

# Set environment defaults.
ENV XLISTMAN_DATABASE_PATH=/data/xlistman.db

ENTRYPOINT ["/xlistman"]
CMD ["serve"]

.PHONY: build test clean docker

# Frontend build (SvelteKit SPA -> static files)
web/build:
	cd web && npm install && npm run build

# Build the Go binary (frontend must be built first)
build: web/build
	go build -ldflags "-X github.com/barats/xlistman/cmd.Version=$(shell git describe --tags --always 2>/dev/null || echo dev)" -o xlistman .

# Build without frontend (backend only)
build-go:
	go build -o xlistman .

# Run tests
test:
	go test ./... -v

# Run tests (no verbose)
test-short:
	go test ./...

# Build Docker image
docker:
	docker build -t xlistman .

# Clean build artifacts
clean:
	rm -f xlistman
	rm -rf web/build

# Generate default config
config:
	./xlistman config init

# Run the daemon (requires config)
serve:
	./xlistman serve

# Tidy Go modules
tidy:
	go mod tidy

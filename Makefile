.PHONY: build build-go web test test-short docker clean config serve tidy

# Frontend build (SvelteKit SPA -> static files)
web:
	cd web && pnpm install && pnpm build

# Build the Go binary with the embedded frontend (frontend must be built first)
build: web
	go build -ldflags "-X github.com/barats/xlistman/cmd.Version=$(shell git describe --tags --always 2>/dev/null || echo dev)" -o xlistman .

# Build without the frontend (backend only)
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
	mkdir -p web/build
	touch web/build/.gitkeep

# Generate default config
config:
	./xlistman config init

# Run the daemon (requires config)
serve:
	./xlistman serve

# Tidy Go modules
tidy:
	go mod tidy

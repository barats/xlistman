.PHONY: build build-go web test test-short docker clean config serve tidy e2e e2e-stop e2e-summary

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

# Frontend e2e suite: bootstrap the environment (build, fresh DB, seed, serve)
# and print the agent prompt. The agent then executes web/tests/*.md against
# Chrome via the chrome-devtools MCP server; report with `make e2e-summary`.
# Run `make web` first if you changed web/src since the last build.
e2e:
	./scripts/e2e.sh setup

e2e-stop:
	./scripts/e2e.sh stop

e2e-summary:
	./scripts/e2e.sh summary

# Tidy Go modules
tidy:
	go mod tidy

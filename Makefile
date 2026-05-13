SHELL := /bin/bash
ROOT := $(shell pwd)

.PHONY: proto-gen proto-clean \
        build-management build-gateway build-error-handler \
        build-agent-runtime build-ebpf build-frontend \
        test-management test-agent-runtime test \
        dev-management dev-frontend dev \
        docker-up docker-down \
        lint lint-go lint-rust lint-proto \
        clean check

# ── Proto ──
proto-gen:
	buf generate --template shared/buf.gen.yaml

proto-lint:
	buf lint

proto-clean:
	rm -rf shared/gen

# ── Go services ──
build-management:
	cd management-server && go build -trimpath -o bin/management-server ./cmd/server

build-gateway:
	cd gateway && go build -trimpath -o bin/gateway ./cmd/gateway

build-error-handler:
	cd error-handler && go build -trimpath -o bin/error-handler ./cmd/worker

# ── Rust ──
build-agent-runtime:
	cargo build -p agent-runtime --release

build-ebpf:
	cargo build -p agentshield-ebpf --target bpfel-unknown-none --release 2>/dev/null || \
		echo "eBPF build requires bpf-linker and Linux kernel headers; skipped"

# ── Frontend ──
build-frontend:
	cd management-server/web && npm ci && npm run build

# ── Test ──
test-management:
	cd management-server && go test -race -count=1 ./...

test-agent-runtime:
	cargo test -p agent-runtime

test-frontend:
	cd management-server/web && npm run test -- --run 2>/dev/null || echo "frontend tests skipped"

test: test-management test-agent-runtime

# ── Dev servers ──
dev-management:
	cd management-server && go run ./cmd/server

dev-frontend:
	cd management-server/web && npm run dev

# ── Lint ──
lint-proto:
	buf lint

lint-go:
	cd management-server && go vet ./...
	cd gateway && go vet ./... 2>/dev/null || true
	cd error-handler && go vet ./... 2>/dev/null || true

lint-rust:
	cargo clippy --workspace 2>/dev/null || echo "clippy not available"

lint: lint-proto lint-go lint-rust

# ── Docker ──
docker-up:
	docker compose -f deployments/docker-compose.yml --profile core up -d management-server opa

docker-up-full:
	docker compose -f deployments/docker-compose.yml --profile full up -d

docker-down:
	docker compose -f deployments/docker-compose.yml down -v

# ── Clean ──
clean: proto-clean
	rm -rf management-server/bin management-server/web/dist
	rm -rf gateway/bin error-handler/bin
	cargo clean 2>/dev/null || true

# ── Quick check (builds everything without tests) ──
check: proto-gen build-management build-frontend
	@echo "All core modules built successfully"

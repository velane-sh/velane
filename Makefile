.PHONY: up down build logs seed tidy copy-platform-libs test-platform-libs openapi-check sandbox-services-build sandbox-services-test sandbox-artifacts help

## tidy: run go mod tidy to generate/update go.sum (required before first build)
tidy:
	cd services/control-plane && go mod tidy

## up: pull latest GHCR images and start all services
up:
	docker compose pull
	docker compose up -d

## dev: build images and start all services in detached mode for local development
dev:
	docker compose -f docker-compose.dev.yml up --build -d

## down: stop and remove containers, keeping volumes (data survives)
down:
	docker compose down
	docker compose -f docker-compose.dev.yml down

## down-clean: stop containers AND wipe all volumes (full reset — re-runs Nango setup)
down-clean:
	docker compose down -v
	docker compose -f docker-compose.dev.yml down -v

## logs: stream control-plane logs
logs:
	docker compose logs -f control-plane

## copy-platform-libs: sync platform-libraries/ into the embedded files directory
copy-platform-libs:
	rm -rf services/control-plane/internal/platformlibs/files/bun \
	       services/control-plane/internal/platformlibs/files/python
	cp -r platform-libraries/bun  services/control-plane/internal/platformlibs/files/
	cp -r platform-libraries/python services/control-plane/internal/platformlibs/files/

## build: compile the control-plane binary locally (requires Go 1.26+ and copy-platform-libs)
build: copy-platform-libs
	cd services/control-plane && go build ./...

## sandbox-services-build: compile each standalone sandbox service module
sandbox-services-build:
	@for service in sandbox-host-agent sandbox-host-watchdog sandbox-guest-init sandbox-image-builder; do \
		echo "--- Building $$service ---"; \
		(cd services/$$service && go build ./...); \
	done

## sandbox-services-test: run ordinary (non-privileged) tests for standalone sandbox services
sandbox-services-test:
	@for service in sandbox-host-agent sandbox-host-watchdog sandbox-guest-init sandbox-image-builder; do \
		echo "--- Testing $$service ---"; \
		(cd services/$$service && go test -count=1 ./...); \
	done

## sandbox-artifacts: build local OCI packaging artifacts for sandbox AMI, rootfs, and builder workflows
sandbox-artifacts:
	docker build -f services/sandbox-host-agent/Dockerfile -t velane-sandbox-host-agent:local services/sandbox-host-agent
	docker build -f services/sandbox-host-watchdog/Dockerfile -t velane-sandbox-host-watchdog:local services/sandbox-host-watchdog
	docker build -f services/sandbox-guest-init/Dockerfile -t velane-sandbox-guest-init:local services/sandbox-guest-init
	docker build -f services/sandbox-image-builder/Dockerfile -t velane-sandbox-image-builder:local services/sandbox-image-builder

## test-platform-libs: run unit tests for all platform libraries (no Salesforce credentials needed)
##   Requires: bun (https://bun.sh) and python3 with pytest (pip install pytest)
test-platform-libs:
	@echo "--- Bun tests ---"
	cd platform-libraries/bun && bun test
	@echo "--- Python tests ---"
	cd platform-libraries/python && python3 -m pytest -v

## openapi-check: validate the OpenAPI document and verify it matches the chi router
openapi-check:
	cd services/control-plane && go test ./internal/api -run TestOpenAPI -count=1

## setup-nango: one-time Nango account setup. Run once after make up on a fresh stack.
##              Prints NANGO_SECRET_KEY and NANGO_PUBLIC_KEY — store in your secrets manager.
##              Idempotent: safe to re-run, returns existing keys if account already exists.
setup-nango:
	@bash scripts/setup-nango.sh

## seed: create a demo tenant (no auth required — bootstrap endpoint)
##
## After seeding, create an API key with:
##   curl -s -X POST http://localhost:8080/v1/tenant/api-keys \
##     -H "Authorization: Bearer <admin-key>" \
##     -H "Content-Type: application/json" \
##     -d '{"name":"default","scopes":["admin","manage","invoke"]}' | jq .
seed:
	@echo "--- Creating demo tenant ---"
	@curl -sf -X POST http://localhost:8080/v1/tenants \
	  -H "Content-Type: application/json" \
	  -d '{"name":"Demo Org","slug":"demo"}' | jq .

## help: list available targets
help:
	@grep -E '^## ' Makefile | sed 's/^## //'

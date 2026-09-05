.DEFAULT_GOAL := all

.PHONY: all help generate build web test benchmark coverage format check lint lint-ci check-naming check-secrets clean watch docker \
	dev dev-backend dev-frontend dev-up dev-down dev-logs dev-shell dev-clean test-deployment check-catalogs check-queries \
	check-config-docs

GO ?= go
NPM ?= npm
WEB_DIR ?= web
BUILD_DIR ?= build
BINARY ?= $(BUILD_DIR)/teanode
SERVER_BINARY ?= $(BUILD_DIR)/teanode-server
DOCKER_TAG ?= teanode:latest
DEV_COMPOSE ?= docker compose -f deploy/docker-compose.dev.yml
DEV_ENVIRONMENT ?= dev/.env
GOLANGCI_LINT ?= golangci-lint

# Pinned rather than @latest. A new linter release adds checks, and with
# @latest that turns a green build red with nobody having changed any code —
# which is how five findings appeared here between one commit and the next.
# Raise it deliberately, and fix what the new version finds in that commit.
GOLANGCI_LINT_VERSION ?= v2.13.1

# Explicit package list: ./... would also pick up stray Go files vendored
# inside web/node_modules by npm packages.
GOPACKAGES ?= ./cmd/... ./internal/...

VERSION ?= $(shell git describe --tags 2>/dev/null || echo 0.1.0)
COMMIT ?= $(shell git describe --match=NeVeRmAtCh --always --abbrev=40 --dirty)
LDFLAGS := -s -w -extldflags "-static" \
	-X github.com/ziyan/teanode/internal/version.version=$(VERSION) \
	-X github.com/ziyan/teanode/internal/version.commit=$(COMMIT)

GOFMTARGS := $(shell find . -mindepth 1 -maxdepth 1 -type d -not -path ./vendor -not -path ./web -not -path ./.git) \
	$(shell find . -mindepth 1 -maxdepth 1 -type f -iname '*.go')

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "; print "Targets:"} /^[a-zA-Z0-9_.-]+:.*## / {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

all: format build test ## Format, build and test

generate: ## Run code generators
	@CGO_ENABLED=0 $(GO) generate -mod=vendor $(GOPACKAGES)

web: $(WEB_DIR)/node_modules ## Build the dashboard into internal/frontend/static
	cd $(WEB_DIR) && $(NPM) run build

$(WEB_DIR)/node_modules: $(WEB_DIR)/package.json $(WEB_DIR)/package-lock.json
	cd $(WEB_DIR) && $(NPM) ci
	@touch $@

build: generate ## Build teanode (the client) and teanode-server
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GO) build -mod=vendor -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/teanode
	CGO_ENABLED=0 $(GO) build -mod=vendor -ldflags '$(LDFLAGS)' -o $(SERVER_BINARY) ./cmd/teanode-server

format: ## Format Go code
	@gofmt -l -w $(GOFMTARGS)

check: ## Fail if code is not formatted
	@if [ -n "$$(gofmt -l -e $(GOFMTARGS))" ]; then \
		gofmt -l -e -d $(GOFMTARGS); \
		echo "ERROR: run 'make format' before committing" >&2; \
		exit 1; \
	fi

lint: lint-ci check-naming ## Run every linter (what to run before committing)

check-secrets: ## Fail if a secret or private reference is in a tracked file
	@scripts/check-secrets.bash

check-config-docs: ## Fail if a configuration field is not documented
	@scripts/check-config-docs.bash

lint-ci: check check-secrets check-catalogs check-config-docs ## Run the linters CI runs
	@set -e; \
	if ! hash $(GOLANGCI_LINT) >/dev/null 2>&1; then \
		$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); \
	fi; \
	$(GOLANGCI_LINT) run $(GOPACKAGES)

# The naming, receiver and error-prefix conventions this project writes down,
# checked mechanically. Configured by .gogolint.yaml.
#
# A local-only check: CI does not run it, so a contributor who does not have
# the checker installed is never blocked by it, and this target says so and
# carries on rather than failing.
check-naming: ## Run the local naming and error-prefix checks
	@if hash gogolint >/dev/null 2>&1; then \
		gogolint $(GOPACKAGES); \
	else \
		echo "the naming checker is not installed; skipping the naming and error-prefix checks."; \
	fi

# -race, because CI runs -race and a test suite that passes here and fails
# there is a test suite nobody trusts. It found a race in a test that mutated
# a package variable in parallel, which this target had reported as passing.
test: generate ## Run tests under the race detector (starts a PostgreSQL container when docker is available)
	@set -e; \
	mkdir -p $(BUILD_DIR); \
	if ! hash gotestsum >/dev/null 2>&1; then \
		$(GO) install gotest.tools/gotestsum@latest; \
	fi; \
	if hash docker >/dev/null 2>&1; then \
		POSTGRES_CONTAINER="$$(docker run -d --rm \
			-e POSTGRES_DB=teanode \
			-e POSTGRES_USER=teanode \
			-e POSTGRES_PASSWORD=teanode \
			-e POSTGRES_HOST_AUTH_METHOD=trust \
			postgres)"; \
		trap "docker kill $${POSTGRES_CONTAINER} >/dev/null 2>&1" EXIT; \
		until docker exec $${POSTGRES_CONTAINER} pg_isready >/dev/null 2>&1; do sleep 1; done; \
		export TEANODE_TEST_DATABASE_HOST="$$(docker inspect --format '{{ range .NetworkSettings.Networks }}{{ .IPAddress }}{{ end }}' $${POSTGRES_CONTAINER})"; \
	fi; \
	gotestsum --format testname -- -mod=vendor -race -cover -coverprofile=$(BUILD_DIR)/coverage.out $(GOPACKAGES); \
	$(GO) tool cover -func=$(BUILD_DIR)/coverage.out | tail -1

# --- development ------------------------------------------------------------

dev: dev-up dev-backend ## Start the dev services and run the server

dev-up: ## Start PostgreSQL and MinIO for development (PROFILE=full also starts ClamAV and SpamAssassin)
	@$(DEV_COMPOSE) $(if $(PROFILE),--profile $(PROFILE),) up -d
	@$(DEV_COMPOSE) exec -T postgres sh -c 'until pg_isready -U teanode >/dev/null 2>&1; do sleep 1; done'
	@echo "postgres ready on 127.0.0.1:15432, minio on 127.0.0.1:19000, redis on 127.0.0.1:16379"

dev-down: ## Stop the dev services
	@$(DEV_COMPOSE) --profile full down

dev-logs: ## Follow the dev service logs
	@$(DEV_COMPOSE) logs -f

dev-shell: ## Print the command that puts the dev environment in your shell
	@echo "set -a; . ./$(DEV_ENVIRONMENT); set +a"

dev-clean: ## Stop the dev services and delete all development state
	@$(DEV_COMPOSE) --profile full down --volumes
	@rm -rf dev
	@echo "removed dev/ and the development volumes"

dev-backend: build ## Run the server against the dev database, setting it up if needed
	@scripts/dev-config.bash
	@set -a; . ./$(DEV_ENVIRONMENT); set +a; ./$(SERVER_BINARY) run

dev-frontend: $(WEB_DIR)/node_modules ## Run the dashboard dev server, proxying to the dev backend
	cd $(WEB_DIR) && $(NPM) run dev

# --- everything else --------------------------------------------------------

benchmark: ## Run benchmarks
	@$(GO) test -mod=vendor -bench=. $(GOPACKAGES)

coverage: test ## Generate an HTML coverage report
	@$(GO) tool cover -html=$(BUILD_DIR)/coverage.out -o $(BUILD_DIR)/coverage.html

watch: ## Rebuild on source change (requires inotifywait)
	@set -e; \
	while true; do \
		inotifywait --quiet --recursive --event modify --event delete --event move \
			--exclude '(^\./(\.git|vendor|build|web/node_modules)/)' .; \
		$(MAKE) format build || true; \
	done

check-catalogs: $(WEB_DIR)/node_modules ## Check the translations against the English catalogue
	@cd $(WEB_DIR) && node scripts/check-catalogs.mjs

check-queries: $(WEB_DIR)/node_modules ## Validate the dashboard's GraphQL against a running server
	@cd $(WEB_DIR) && TEANODE_URL=$(or $(TEANODE_URL),http://127.0.0.1:8833) node scripts/check-queries.mjs

test-deployment: ## Bring the whole stack up in Docker and prove it works end to end
	@scripts/test-deployment.bash

docker: ## Build the container image, dashboard included
	@docker build -t $(DOCKER_TAG) -f deploy/Dockerfile \
		--build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) .

clean: ## Remove build artifacts
	rm -rf $(BUILD_DIR)
	rm -rf $(WEB_DIR)/node_modules
	rm -rf internal/frontend/static

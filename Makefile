# Hippo: single entry point for local commands. The CI calls the same targets.
# Run `make` (or `make help`) to list them.

SHELL := /bin/bash
.DEFAULT_GOAL := help

VERSION ?= $(shell git describe --always --dirty 2>/dev/null || echo dev)

# Generated files: never edited by hand, checked up to date by `make check-generated`.
GENERATED := backend/internal/httpapi/api.gen.go \
	backend/internal/analysis/schema/classify-item.bundled.json \
	app/src/api/schema.gen.ts

# infra/.env is optional for the local targets: without it, variables come from the shell.
LOCAL_ENV_FILE := infra/.env
LOAD_LOCAL_ENV := set -a; if [ -f $(LOCAL_ENV_FILE) ]; then . ./$(LOCAL_ENV_FILE); fi; set +a

LYCHEE_FLAGS := --offline --no-progress --include-fragments --hidden \
	--exclude-path node_modules --exclude-path .claude/worktrees --exclude-path .git

.PHONY: help
help: ## List the available targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# --- Setup -------------------------------------------------------------------

.PHONY: setup
setup: ## Install the git hooks and the project dependencies (once per clone)
	lefthook install
	cd api && npm ci --no-fund --no-audit
	cd app && npm ci --no-fund --no-audit

# --- Aggregate ---------------------------------------------------------------

.PHONY: check
check: check-secrets check-docs check-tooling check-openspec lint-api check-schemas check-backend check-app check-infra check-generated ## Run every check the CI runs

# --- Code generation -----------------------------------------------------------

.PHONY: generate
generate: ## Regenerate the code built from api/openapi.yaml and docs/prompt/schemas/
	cd backend && go generate ./...
	cd api && npm run --silent generate

.PHONY: check-generated
check-generated: generate ## Fail if a generated file is not up to date with its source
	@if [ -n "$$(git status --porcelain -- $(GENERATED))" ]; then \
		git --no-pager diff --stat -- $(GENERATED); \
		echo "Generated files are out of date: run 'make generate' and commit the result."; \
		exit 1; \
	fi

.PHONY: fmt
fmt: ## Format the Go code (gofumpt, goimports) and the app (Prettier)
	cd backend && golangci-lint fmt ./...
	cd app && npx --no-install prettier --write --log-level warn .

# --- Repository-wide checks --------------------------------------------------

.PHONY: check-secrets
check-secrets: ## Scan the git history for secrets
	gitleaks git --redact --no-banner

.PHONY: check-docs
check-docs: ## Check the internal links of the Markdown files
	lychee $(LYCHEE_FLAGS) './**/*.md'

.PHONY: check-tooling
check-tooling: ## Test the agent hooks and lint the shell scripts
	bash .claude/hooks/block-git-writes.test.sh
	bash .claude/hooks/protect-files.test.sh
	shellcheck -S warning .claude/hooks/protect-files.sh .claude/hooks/protect-files.test.sh .claude/hooks/format-on-edit.sh
	shellcheck -S error .claude/hooks/block-git-writes.sh .claude/hooks/block-git-writes.test.sh
	actionlint

.PHONY: check-openspec
check-openspec: ## Validate every OpenSpec spec and change
	openspec validate --all --no-interactive

# --- API contract ------------------------------------------------------------

.PHONY: lint-api
lint-api: ## Lint the OpenAPI contract (Redocly)
	cd api && npx --no-install redocly lint openapi.yaml

# --- Backend -----------------------------------------------------------------

.PHONY: check-backend
check-backend: lint-backend test-backend build-backend ## Lint, test and build the backend

.PHONY: lint-backend
lint-backend: ## Lint the Go code (golangci-lint, depguard architecture rules)
	cd backend && golangci-lint run ./...

.PHONY: test-backend
test-backend: ## Run the Go tests (unit and integration on a real SQLite), with the race detector
	cd backend && go test -race -cover ./...

.PHONY: build-backend
build-backend: ## Build the backend binary into backend/bin/
	cd backend && CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/hippo ./cmd/hippo

.PHONY: dev
dev: ## Run the backend locally on 127.0.0.1:8080 (variables from infra/.env if present)
	@mkdir -p data/dev
	@$(LOAD_LOCAL_ENV); cd backend && HTTP_ADDR=127.0.0.1:8080 DATA_DIR=../data/dev go run -ldflags "-X main.version=$(VERSION)" ./cmd/hippo

.PHONY: image
image: ## Build the backend Docker image (hippo:local)
	docker build --build-arg VERSION=$(VERSION) -t hippo:local backend

# --- Fiche schemas -----------------------------------------------------------

.PHONY: check-schemas
check-schemas: ## Validate the fiche examples and check the bundled schema is up to date
	cd backend && go run ./cmd/schemabundle -schemas ../docs/prompt/schemas \
		-examples ../docs/prompt/schemas/examples \
		-out internal/analysis/schema/classify-item.bundled.json -check

# --- App ---------------------------------------------------------------------

EXPO_DOCTOR := expo-doctor@1.21.5

.PHONY: check-app
check-app: lint-app typecheck-app test-app ## Lint, type-check and test the app

.PHONY: lint-app
lint-app: ## Lint the app (ESLint, Prettier)
	cd app && npx --no-install eslint . --max-warnings=0
	cd app && npx --no-install prettier --check --log-level warn .

.PHONY: typecheck-app
typecheck-app: ## Type-check the app (tsc --noEmit)
	cd app && npx --no-install tsc --noEmit

.PHONY: test-app
test-app: ## Run the app tests (jest-expo, React Native Testing Library)
	cd app && npx --no-install jest --coverage --coverageReporters=text-summary

.PHONY: doctor-app
doctor-app: ## Check the Expo project configuration (expo-doctor)
	cd app && npx --yes $(EXPO_DOCTOR)

.PHONY: app
app: ## Start the Expo dev server for the development build (HIPPO_API_URL, HIPPO_API_TOKEN)
	cd app && npx --no-install expo start --dev-client

# --- Local stack (Docker Compose, without Tailscale) ----------------------------

COMPOSE_LOCAL := docker compose -f infra/docker-compose.local.yml

.PHONY: up
up: ## Build and start the local stack on 127.0.0.1:8080 (variables from infra/.env or the shell)
	VERSION=$(VERSION) $(COMPOSE_LOCAL) up --build --detach --wait

.PHONY: down
down: ## Stop the local stack (the data stays in infra/data/)
	API_TOKEN=$${API_TOKEN:-unused-by-down} $(COMPOSE_LOCAL) down

.PHONY: logs
logs: ## Follow the logs of the local stack
	API_TOKEN=$${API_TOKEN:-unused-by-logs} $(COMPOSE_LOCAL) logs --follow

.PHONY: check-infra
check-infra: ## Validate both compose files
	API_TOKEN=check TS_AUTHKEY=check docker compose -f infra/docker-compose.yml config --quiet
	API_TOKEN=check $(COMPOSE_LOCAL) config --quiet

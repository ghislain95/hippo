# Hippo: single entry point for local commands. The CI calls the same targets.
# Run `make` (or `make help`) to list them.

SHELL := /bin/bash
.DEFAULT_GOAL := help

LYCHEE_FLAGS := --offline --no-progress --include-fragments \
	--exclude-path node_modules --exclude-path .claude/worktrees

.PHONY: help
help: ## List the available targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# --- Setup -------------------------------------------------------------------

.PHONY: setup
setup: ## Install the git hooks and the project dependencies (once per clone)
	lefthook install

# --- Aggregate ---------------------------------------------------------------

.PHONY: check
check: check-secrets check-docs ## Run every check the CI runs

# --- Repository-wide checks --------------------------------------------------

.PHONY: check-secrets
check-secrets: ## Scan the git history for secrets
	gitleaks git --redact --no-banner

.PHONY: check-docs
check-docs: ## Check the internal links of the Markdown files
	lychee $(LYCHEE_FLAGS) './**/*.md'

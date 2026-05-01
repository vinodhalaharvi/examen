# =============================================================================
# Examen — multi-agent exam prep
#
# Common targets:
#
#   make help              show this help (default)
#   make build             compile all binaries into ./bin
#   make test              run all tests
#   make generate          generate questions with mock LLM
#   make generate-real     generate questions with real Claude API
#   make serve             run the web server
#   make tunnel            expose the local server via cloudflared
#   make clean             remove generated artifacts
#   make reset-db          delete the SQLite database (keeps .bak)
#   make migrate           import data/bank.json into data/examen.db
#
# Configuration via environment (or .env.local, sourced automatically):
#   ANTHROPIC_API_KEY              required for `make generate-real`
#   GOOGLE_OAUTH_CLIENT_ID         required for Google sign-in
#   GOOGLE_OAUTH_CLIENT_SECRET     required for Google sign-in
#   GOOGLE_OAUTH_REDIRECT_URL      required for Google sign-in
#   EXAMEN_COOKIE_KEY              required for Google sign-in
#   COUNT                          number of questions to generate (default 30)
#   ADDR                           server listen address (default :8080)
#   BANK                           SQLite path (default data/examen.db)
# =============================================================================

# Auto-load .env.local if present
ifneq (,$(wildcard ./.env.local))
    include .env.local
    export
endif

# Defaults — override on the command line: `make generate COUNT=10`
COUNT ?= 30
ADDR  ?= :8080
BANK  ?= data/examen.db

GO      ?= go
BIN_DIR := bin
PKGS    := ./...

# Detect cloudflared, but don't require it
CLOUDFLARED := $(shell command -v cloudflared 2>/dev/null)

.PHONY: help
help:
	@echo "Examen — multi-agent exam prep"
	@echo ""
	@echo "Targets:"
	@echo "  build              compile all binaries into ./bin"
	@echo "  test               run all tests"
	@echo "  test-verbose       run all tests with verbose output"
	@echo "  generate           generate questions with mock LLM (no API key needed)"
	@echo "  generate-real      generate questions with real Claude API"
	@echo "  serve              run the web server (anonymous mode if no auth env)"
	@echo "  tunnel             expose local server via cloudflared (HTTPS public URL)"
	@echo "  migrate            import legacy data/bank.json into data/examen.db"
	@echo "  reset-db           delete examen.db and journal files (keeps .bak)"
	@echo "  clean              remove ./bin and any temp files"
	@echo "  fmt                run gofmt on the codebase"
	@echo "  vet                run go vet"
	@echo "  check              run fmt + vet + test"
	@echo ""
	@echo "Common workflows:"
	@echo "  make generate && make serve         # demo with mock data"
	@echo "  make generate-real COUNT=10         # real Claude, small batch"
	@echo "  make serve & make tunnel            # public demo via cloudflared"

# =============================================================================
# BUILD
# =============================================================================

.PHONY: build
build: $(BIN_DIR)/serve $(BIN_DIR)/generate $(BIN_DIR)/migrate-bank

$(BIN_DIR)/serve: $(shell find . -name '*.go' -not -path './bin/*' -not -path './.git/*' 2>/dev/null)
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/serve ./cmd/serve

$(BIN_DIR)/generate: $(shell find . -name '*.go' -not -path './bin/*' -not -path './.git/*' 2>/dev/null)
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/generate ./cmd/generate

$(BIN_DIR)/migrate-bank: $(shell find . -name '*.go' -not -path './bin/*' -not -path './.git/*' 2>/dev/null)
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/migrate-bank ./cmd/migrate-bank

# =============================================================================
# TEST / CHECK
# =============================================================================

.PHONY: test
test:
	$(GO) test $(PKGS)

.PHONY: test-verbose
test-verbose:
	$(GO) test -v $(PKGS)

.PHONY: fmt
fmt:
	gofmt -w -s $(shell find . -name '*.go' -not -path './bin/*' -not -path './.git/*')

.PHONY: vet
vet:
	$(GO) vet $(PKGS)

.PHONY: check
check: fmt vet test
	@echo "✓ all checks passed"

# =============================================================================
# DATA — generation, migration, inspection
# =============================================================================

# Ensure data dir exists for any target that writes to it
data:
	@mkdir -p data

.PHONY: generate
generate: data
	$(GO) run ./cmd/generate -count $(COUNT) -bank $(BANK)

.PHONY: generate-real
generate-real: data
	@if [ -z "$$ANTHROPIC_API_KEY" ]; then \
		echo "ERROR: ANTHROPIC_API_KEY not set. Add it to .env.local or export it."; \
		exit 1; \
	fi
	$(GO) run ./cmd/generate -real -count $(COUNT) -bank $(BANK)

.PHONY: migrate
migrate: data
	@if [ ! -f data/bank.json ]; then \
		echo "data/bank.json not found — nothing to migrate"; \
		exit 1; \
	fi
	$(GO) run ./cmd/migrate-bank -from data/bank.json -to $(BANK)

.PHONY: reset-db
reset-db:
	@if [ -f $(BANK) ]; then \
		echo "Removing $(BANK), $(BANK)-wal, $(BANK)-shm (.bak preserved)"; \
		rm -f $(BANK) $(BANK)-wal $(BANK)-shm; \
	else \
		echo "$(BANK) does not exist; nothing to remove"; \
	fi

# =============================================================================
# SERVE
# =============================================================================

.PHONY: serve
serve:
	@if [ ! -f $(BANK) ]; then \
		echo "ERROR: $(BANK) does not exist. Run 'make generate' first."; \
		exit 1; \
	fi
	$(GO) run ./cmd/serve -addr $(ADDR) -bank $(BANK)

.PHONY: serve-real
serve-real:
	@if [ ! -f $(BANK) ]; then \
		echo "ERROR: $(BANK) does not exist. Run 'make generate' first."; \
		exit 1; \
	fi
	$(GO) run ./cmd/serve -real -addr $(ADDR) -bank $(BANK)

.PHONY: tunnel
tunnel:
ifndef CLOUDFLARED
	@echo "ERROR: cloudflared not found in PATH"
	@echo "Install with: brew install cloudflared"
	@exit 1
endif
	@echo "Starting tunnel to http://localhost$(ADDR) ..."
	@echo "(make sure 'make serve' is running in another terminal)"
	cloudflared tunnel --url http://localhost$(ADDR)

# =============================================================================
# CLEAN
# =============================================================================

.PHONY: clean
clean:
	rm -rf $(BIN_DIR)
	@echo "✓ cleaned"

.PHONY: clean-all
clean-all: clean reset-db
	@echo "✓ all artifacts removed (data/*.bak preserved)"

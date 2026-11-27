# GitHub-hosted Ubuntu uses dash as /bin/sh (no pipefail); keep recipes on bash.
SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c

REQUIRED_TOOLS := goimports gofumpt govulncheck gosec golangci-lint gocognit nilaway
NILAWAY        := nilaway
# Analyze first-party packages; report errors only in editable trees.
NILAWAY_INCLUDE_ERRORS := $(CURDIR),$(CURDIR)/cmd,$(CURDIR)/codec,$(CURDIR)/picker,$(CURDIR)/resolver,$(CURDIR)/telemetry,$(CURDIR)/internal
NILAWAY_FLAGS  := -include-pkgs=github.com/yeqown/memcached -include-errors-in-files=$(NILAWAY_INCLUDE_ERRORS) -exclude-test-files -pretty-print=false -print-full-file-path
GOIMPORTS      := goimports -local github.com/yeqown/memcached
GOFUMPT        := gofumpt -extra=group_params,clothe_returns,balance_calls
GOVULNCHECK    := govulncheck
GOSEC          := gosec
GOLANGCI_LINT  := golangci-lint
GOCOGNIT       := gocognit
# Cognitive complexity ceiling (functions with score > N fail).
# Matches golangci-lint gocognit default (min-complexity: 30).
# See https://github.com/uudashr/gocognit
GOCOGNIT_OVER  ?= 30

# Root module packages (library + internal helpers).
GO_PACKAGES := ./...

.PHONY: check fmt lint test test-integration coverage pre-commit docker-up docker-down clean install \
	gui-dev gui-build gui-test gui-clean \
	_tools _fmt-check _vet _lint _cognit _nilaway _security _vuln _test

## check — run before every commit
check: _fmt-check _vet _lint _cognit _nilaway _security _vuln _test

## fmt — rewrite Go sources with goimports + gofumpt extra rules
fmt: _tools
	$(GOIMPORTS) -w $$(go list -f '{{.Dir}}' $(GO_PACKAGES))
	$(GOFUMPT) -w $$(go list -f '{{.Dir}}' $(GO_PACKAGES))

_tools:
	@missing=0; unsupported=0; \
	for tool in $(REQUIRED_TOOLS); do \
		if command -v "$$tool" >/dev/null 2>&1; then \
			tool_path=$$(command -v "$$tool"); \
			build_go=$$(go version -m "$$tool_path" 2>/dev/null | awk 'NR == 1 { sub(/^.*: /, ""); print }'); \
			minor=$${build_go#go1.}; minor=$${minor%%.*}; \
			if [ "$${build_go#go1.}" = "$$build_go" ] || [ -z "$$minor" ] || [ "$$minor" -lt 26 ]; then \
				echo "tools: $$tool unsupported (built with $${build_go:-unknown}; need go1.26+)"; \
				unsupported=1; \
			else \
				echo "tools: $$tool ok ($$build_go)"; \
			fi; \
		else \
			echo "tools: $$tool missing"; \
			missing=1; \
		fi; \
	done; \
	[ "$$missing" -eq 0 ] || { \
		echo "tools: install missing tools with: go install / brew install $(REQUIRED_TOOLS)"; \
		exit 1; \
	}; \
	[ "$$unsupported" -eq 0 ] || { \
		echo "tools: rebuild or reinstall tools with Go 1.26 or newer"; \
		exit 1; \
	}

_fmt-check: _tools
	@fail=0; \
	for dir in $$(go list -f '{{.Dir}}' $(GO_PACKAGES)); do \
		for f in "$$dir"/*.go; do \
			[ -f "$$f" ] || continue; \
			if [ -n "$$($(GOIMPORTS) -l "$$f")" ]; then echo "goimports: $$f"; fail=1; fi; \
			if [ -n "$$($(GOFUMPT) -l "$$f")" ]; then echo "gofumpt: $$f"; fail=1; fi; \
		done; \
	done; \
	[ $$fail -eq 0 ] || { echo "fmt-check: formatting differences found"; exit 1; }

_vet:
	go vet $(GO_PACKAGES)

_lint: _tools
	$(GOLANGCI_LINT) run $(GO_PACKAGES)

_cognit: _tools
	$(GOCOGNIT) -over $(GOCOGNIT_OVER) -ignore '_test\.go$$' $$(go list -f '{{.Dir}}' $(GO_PACKAGES))

_nilaway: _tools
	$(NILAWAY) $(NILAWAY_FLAGS) ./...

_security: _tools
	# Limit to the root library module; nested modules under cmd/example/benchmark stay out of scope.
	$(GOSEC) -exclude-dir=cmd -exclude-dir=example -exclude-dir=benchmark ./...

_vuln: _tools
	$(GOVULNCHECK) ./...

_test:
	go test -race -count=1 $(GO_PACKAGES)

lint:
	@GOTOOLCHAIN=go1.26.0 go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run ./...

test:
	@echo "Running tests"
	@go test -v -race ./...

test-integration:
	@test -n "$(MEMCACHED_TEST_ADDR)" || { echo "Set MEMCACHED_TEST_ADDR to a dedicated Memcached host:port" >&2; exit 1; }
	@MEMCACHED_TEST_ADDR="$(MEMCACHED_TEST_ADDR)" go test -v -race -run '^TestMemcachedIntegration$$' -count=1 .

coverage:
	@echo "Running tests with coverage"
	@go test -v -race -coverprofile=coverage.txt -covermode=atomic ./...
	@go tool cover -html=coverage.txt -o coverage.html

pre-commit:
	@echo "Running pre-commit"
	@pre-commit run --all-files

docker-up:
	@echo "Starting memcached container"
	@docker run -d --name memcached-test -p 127.0.0.1:11211:11211 memcached:1.6.37

docker-down:
	@echo "Stopping memcached container"
	@docker stop memcached-test
	@docker rm memcached-test

clean:
	@echo "Cleaning build artifacts"
	@rm -f coverage.txt coverage.html
	@go clean -testcache

install:
	@echo "Installing memcached-cli into `go env GOBIN`"
	@go install ./cmd/memcached-cli

# GUI targets
gui-dev:
	@echo "Starting GUI dev server"
	@cd cmd/gui && wails dev

gui-build:
	@echo "Building GUI application"
	@cd cmd/gui && wails build

gui-test:
	@echo "Running GUI backend tests"
	@cd cmd/gui && go test -v -race ./service/...

gui-clean:
	@echo "Cleaning GUI build artifacts"
	@cd cmd/gui && rm -rf build/bin frontend/dist

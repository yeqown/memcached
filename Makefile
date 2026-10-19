.PHONY: lint test test-integration coverage pre-commit docker-up docker-down clean install \
        gui-dev gui-build gui-test gui-clean

lint:
	@GOTOOLCHAIN=go1.26.0 go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./...

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

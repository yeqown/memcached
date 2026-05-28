.PHONY: lint test coverage pre-commit docker-up docker-down clean install \
        gui-dev gui-build gui-test gui-clean

lint:
	@GOTOOLCHAIN=go1.26.0 go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8 run ./...

test:
	@echo "Running tests"
	@go test -v -race ./...

coverage:
	@echo "Running tests with coverage"
	@go test -v -race -coverprofile=coverage.txt -covermode=atomic ./...
	@go tool cover -html=coverage.txt -o coverage.html

pre-commit:
	@echo "Running pre-commit"
	@pre-commit run --all-files

docker-up:
	@echo "Starting memcached container"
	@docker run -d --name memcached-test -p 11211:11211 memcached:latest

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
	@cd gui && wails dev

gui-build:
	@echo "Building GUI application"
	@cd gui && wails build

gui-test:
	@echo "Running GUI backend tests"
	@cd gui && go test -v -race ./service/...

gui-clean:
	@echo "Cleaning GUI build artifacts"
	@cd gui && rm -rf build/bin frontend/dist

.PHONY: lint test coverage pre-commit docker-up docker-down clean

lint:
	@echo "Running golangci-lint"
	@golangci-lint run ./...

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

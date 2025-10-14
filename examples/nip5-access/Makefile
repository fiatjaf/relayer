.PHONY: build run clean test deps help

# Build the NIP-5 access relay
build:
	go build -o nip5-access-relay .

# Run the relay
run:
	go run .

# Run with example environment variables
run-example:
	POSTGRESQL_DATABASE="postgres://postgres:password@localhost/nip5_access_relay?sslmode=disable" \
	RELAY_URL="wss://localhost:7447" \
	ALLOWED_DOMAINS="example.com,example.org" \
	go run .

# Clean build artifacts
clean:
	rm -f nip5-access-relay test-client

# Run tests
test:
	go test -v .

# Install dependencies
deps:
	go mod tidy

# Build test client
build-test-client:
	go build -o test-client ./tests

# Run test client
test-client: build-test-client
	./test-client

# Help
help:
	@echo "Available targets:"
	@echo "  build         - Build the NIP-5 access relay"
	@echo "  run           - Run the relay"
	@echo "  run-example   - Run with example environment variables"
	@echo "  clean         - Clean build artifacts"
	@echo "  test          - Run tests"
	@echo "  deps          - Install dependencies"
	@echo "  build-test-client - Build the test client"
	@echo "  test-client   - Run the test client"
	@echo "  help          - Show this help"




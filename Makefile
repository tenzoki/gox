# Gox Makefile
.PHONY: build build-all fmt vet test clean check install help run

# Default target
all: check build-all

# Build targets
build: ; go build -o gox cmd/gox/main.go
build-all: build

# Development targets
fmt: ; go fmt ./...
vet: ; go vet ./...
test: ; go test ./... -race -count=1
check: fmt vet test

# Demo targets
run: build ; ./gox
run-pipeline: build ; ./gox -config examples/pipeline-demo/pipeline-cell.yaml -debug
run-pubsub: build ; ./gox -config examples/pubsub-demo/pubsub-cell.yaml -debug

# Utility targets
clean: ; rm -f gox
install: build ; cp gox /usr/local/bin/ || sudo cp gox /usr/local/bin/

help:
	@echo "Gox Build System"
	@echo "================="
	@echo ""
	@echo "Build Targets:"
	@echo "  build          - Build gox orchestrator binary"
	@echo "  build-all      - Build all binaries"
	@echo "  all            - Run checks and build all (default)"
	@echo ""
	@echo "Development:"
	@echo "  fmt            - Format Go code"
	@echo "  vet            - Run go vet"
	@echo "  test           - Run tests with race detection"
	@echo "  check          - Run fmt, vet, and test"
	@echo ""
	@echo "Demo Targets:"
	@echo "  run            - Run gox with default config"
	@echo "  run-pipeline   - Run pipeline demo"
	@echo "  run-pubsub     - Run pub/sub demo"
	@echo ""
	@echo "Utilities:"
	@echo "  clean          - Remove built binaries"
	@echo "  install        - Install gox to /usr/local/bin"
	@echo "  help           - Show this help"
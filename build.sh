#!/bin/bash

# Gox Build Script
set -e

echo "Building Gox..."

# Create build directory
mkdir -p build

# Build main orchestrator
echo "Building orchestrator..."
go build -o build/gox ./cmd/gox

# Build standalone orchestrator
echo "Building standalone orchestrator..."
go build -o build/orchestrator ./cmd/orchestrator

# Build operators
echo "Building operators..."
go build -o build/file_ingester ./operators/file_ingester/
go build -o build/text_transformer ./operators/text_transformer/
go build -o build/file_writer ./operators/file_writer/
go build -o build/adapter ./cmd/adapter/

echo "Build complete!"
echo ""
echo "Built files:"
ls -la build/
echo ""
echo "To run the complete system:"
echo "1. Start orchestrator: ./build/gox gox.yaml"
echo "2. In separate terminals, start agents:"
echo "   - ./build/file_ingester"
echo "   - ./build/text_transformer  (external await agent)" 
echo "   - ./build/file_writer        (will be spawned by orchestrator)"
echo ""
echo "Or use the automated test: ./test.sh"
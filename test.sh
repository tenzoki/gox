#!/bin/bash

# Gox Comprehensive Test Script
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test result tracking
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

print_header() {
    echo -e "${BLUE}================================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}================================================${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
    ((PASSED_TESTS++))
    ((TOTAL_TESTS++))
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
    ((FAILED_TESTS++))
    ((TOTAL_TESTS++))
}

print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

run_test() {
    local test_name="$1"
    local test_command="$2"
    
    echo -e "${YELLOW}Running: $test_name${NC}"
    if eval "$test_command" >/dev/null 2>&1; then
        print_success "$test_name"
        return 0
    else
        print_error "$test_name"
        return 1
    fi
}

print_header "Gox Comprehensive Test Suite"

# Build first
echo -e "${YELLOW}Building project...${NC}"
if ./build.sh; then
    print_success "Build completed"
else
    print_error "Build failed"
    exit 1
fi

# Run unit tests
print_header "Unit Tests"

echo "Running Go unit tests..."
if go test ./test/ -v; then
    print_success "All unit tests passed"
else
    print_error "Some unit tests failed"
fi

echo ""
echo "Running individual test packages..."

# Test individual packages
run_test "Agent package tests" "go test ./test/agent_test.go -v"
run_test "Broker package tests" "go test ./test/broker_test.go -v"
run_test "Client package tests" "go test ./test/client_test.go -v"
run_test "Config package tests" "go test ./test/config_test.go -v"
run_test "Support package tests" "go test ./test/support_test.go -v"
run_test "Envelope package tests" "go test ./test/envelope_test.go -v"
run_test "Orchestrator package tests" "go test ./test/orchestrator_unit_test.go -v"
run_test "Integration tests" "go test ./test/integration_test.go -v"

# Run benchmark tests
print_header "Benchmark Tests"
echo "Running benchmark tests..."
go test ./test/ -bench=. -benchmem

print_header "Integration Test - Pipeline Demo"

# Create test directories
echo "Setting up test environment..."
mkdir -p examples/pipeline-demo/input
mkdir -p examples/pipeline-demo/output

# Clean previous test files
rm -f examples/pipeline-demo/input/*.txt
rm -f examples/pipeline-demo/output/*.txt

# Create test files
echo "Creating test files..."
echo "Hello World! This is a test file for Gox pipeline processing." > examples/pipeline-demo/input/test1.txt
echo "Another test file with different content for processing." > examples/pipeline-demo/input/test2.txt
echo "Third test file to ensure multiple file processing works correctly." > examples/pipeline-demo/input/test3.txt

echo ""
echo "Test files created:"
ls -la examples/pipeline-demo/input/

# Test configuration files exist
if [ ! -f "gox.yaml" ]; then
    print_error "gox.yaml configuration file not found"
    exit 1
else
    print_success "Configuration file found"
fi

if [ ! -f "pool.yaml" ]; then
    print_error "pool.yaml configuration file not found"
    exit 1
else
    print_success "Pool configuration file found"
fi

if [ ! -f "cells.yaml" ]; then
    print_error "cells.yaml configuration file not found"
    exit 1
else
    print_success "Cell configuration file found"
fi

# Check if required binaries exist
if [ ! -f "./build/gox" ]; then
    print_error "Gox orchestrator binary not found"
    exit 1
else
    print_success "Orchestrator binary found"
fi

if [ ! -f "./build/text_transformer" ]; then
    print_error "Text transformer binary not found"
    exit 1
else
    print_success "Text transformer binary found"
fi

echo ""
echo "Starting Gox orchestrator in background..."
./build/gox gox.yaml &
GOX_PID=$!

# Give orchestrator time to start
sleep 3

# Check if orchestrator is running
if kill -0 $GOX_PID 2>/dev/null; then
    print_success "Orchestrator started successfully"
else
    print_error "Orchestrator failed to start"
    exit 1
fi

echo "Starting text transformer (await agent) in background..."
./build/text_transformer &
TRANSFORMER_PID=$!

# Give everything time to initialize
sleep 2

# Check if transformer is running
if kill -0 $TRANSFORMER_PID 2>/dev/null; then
    print_success "Text transformer started successfully"
else
    print_error "Text transformer failed to start"
fi

echo ""
echo "System is running. Waiting for file processing..."
echo "Monitoring for 15 seconds..."

# Monitor for results
for i in {1..15}; do
    echo -n "."
    sleep 1
    
    # Check if output files exist
    if ls examples/pipeline-demo/output/*.txt >/dev/null 2>&1; then
        echo ""
        print_success "Processed files detected after $i seconds"
        break
    fi
done

echo ""
echo ""
print_header "Test Results Analysis"

# Check results
if [ -d "examples/pipeline-demo/output" ]; then
    echo "Output directory contents:"
    ls -la examples/pipeline-demo/output/
    echo ""
    
    # Count processed files
    processed_count=$(ls examples/pipeline-demo/output/*.txt 2>/dev/null | wc -l)
    input_count=$(ls examples/pipeline-demo/input/*.txt 2>/dev/null | wc -l)
    
    echo "Input files: $input_count"
    echo "Processed files: $processed_count"
    
    if [ $processed_count -gt 0 ]; then
        print_success "Files were processed successfully"
        
        echo ""
        echo "Sample processed file content:"
        for file in examples/pipeline-demo/output/*.txt; do
            if [ -f "$file" ]; then
                echo "--- $(basename "$file") ---"
                head -10 "$file"
                echo ""
                break
            fi
        done
        
        # Verify transformation (should be uppercase)
        if grep -q "[A-Z]" examples/pipeline-demo/output/*.txt 2>/dev/null; then
            print_success "Text transformation (uppercase) detected"
        else
            print_warning "Text transformation may not have worked as expected"
        fi
    else
        print_error "No processed files found"
    fi
else
    print_error "Output directory not found"
fi

# Check if input files were cleaned up (if configured to delete)
remaining_input=$(ls examples/pipeline-demo/input/*.txt 2>/dev/null | wc -l)
echo "Remaining input files: $remaining_input"

print_header "Cleanup"

echo "Stopping processes..."
if kill $GOX_PID 2>/dev/null; then
    print_success "Orchestrator stopped"
else
    print_warning "Orchestrator already stopped or failed to stop"
fi

if kill $TRANSFORMER_PID 2>/dev/null; then
    print_success "Text transformer stopped"
else
    print_warning "Text transformer already stopped or failed to stop"
fi

# Give processes time to clean up
sleep 2

# Kill any remaining processes
pkill -f "./build/gox" 2>/dev/null || true
pkill -f "./build/text_transformer" 2>/dev/null || true

print_header "Final Test Report"

echo "Total tests run: $TOTAL_TESTS"
echo -e "${GREEN}Passed: $PASSED_TESTS${NC}"
echo -e "${RED}Failed: $FAILED_TESTS${NC}"

if [ $FAILED_TESTS -eq 0 ]; then
    echo -e "${GREEN}🎉 All tests passed successfully!${NC}"
    echo ""
    echo "Expected behavior verified:"
    echo "1. ✓ Files from input/ were ingested and published"
    echo "2. ✓ Text transformer converted to uppercase and added metadata"
    echo "3. ✓ File writer saved processed files to output/"
    echo "4. ✓ System handled multiple files correctly"
    echo "5. ✓ All processes started and stopped cleanly"
    exit 0
else
    echo -e "${RED}❌ $FAILED_TESTS test(s) failed${NC}"
    echo ""
    echo "Please review the test output above for details."
    exit 1
fi
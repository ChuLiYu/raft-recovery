.PHONY: all build test clean run demo install

# 變數定義
BINARY_NAME=beaver-raft
BUILD_DIR=bin
MAIN_PATH=./cmd/queue
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE=$(shell date +%Y-%m-%d_%H:%M:%S)

# 編譯標誌
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

all: clean build test

# 構建二進制文件
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# 運行測試
test:
	@echo "Running tests..."
	go test -v -race ./internal/controller/
	go test -v -race ./internal/jobmanager/
	go test -v -race ./test/integration/
	@echo "All tests passed!"

# 運行基準測試
bench:
	@echo "Running benchmarks..."
	go test -v -bench=. -benchmem ./...

# 清理構建產物
clean:
	@echo "Cleaning..."
	rm -rf $(BUILD_DIR)
	rm -rf ./data
	rm -rf ./internal/storage/wal/test_*.log.*
	@echo "Clean complete"

# 運行服務器
run: build
	@echo "Starting Beaver-Raft server..."
	./$(BUILD_DIR)/$(BINARY_NAME) run

# 運行示例 demo
demo: build
	@echo "Running demo..."
	@mkdir -p data/wal data/snapshot test
	@./scripts/demo.sh

# 提交任務
enqueue: build
	@echo "Enqueuing jobs from test/jobs.json..."
	./$(BUILD_DIR)/$(BINARY_NAME) enqueue --file test/jobs.json

# 查看狀態
status: build
	./$(BUILD_DIR)/$(BINARY_NAME) status

# 安裝依賴
install:
	@echo "Installing dependencies..."
	go mod download
	go mod tidy
	@echo "Dependencies installed"

# 格式化代碼
fmt:
	@echo "Formatting code..."
	go fmt ./...
	@echo "Format complete"

# 靜態分析
vet:
	@echo "Running vet..."
	go vet ./...
	@echo "Vet complete"

# 生成代碼覆蓋報告
coverage:
	@echo "Generating coverage report..."
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# 幫助信息
help:
	@echo "Beaver-Raft Makefile Commands:"
	@echo "  make build    - Build the binary"
	@echo "  make test     - Run all tests"
	@echo "  make bench    - Run benchmarks"
	@echo "  make clean    - Clean build artifacts"
	@echo "  make run      - Build and run server"
	@echo "  make demo     - Run demo script"
	@echo "  make enqueue  - Enqueue test jobs"
	@echo "  make status   - Show system status"
	@echo "  make install  - Install dependencies"
	@echo "  make fmt      - Format code"
	@echo "  make vet      - Run static analysis"
	@echo "  make coverage - Generate coverage report"

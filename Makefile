.PHONY: all build test bench clean proto run docker help

# 版本信息
VERSION := 1.0.0
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.gitCommit=$(GIT_COMMIT)"

# Go 相关
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOMOD := $(GOCMD) mod
GOVET := $(GOCMD) vet
GOFMT := gofmt

# 目录
BIN_DIR := bin
CMD_DIR := cmd/server

# 二进制文件
BINARY_NAME := match-engine

all: build

## build: 编译项目
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BIN_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME) ./$(CMD_DIR)
	@echo "Build complete: $(BIN_DIR)/$(BINARY_NAME)"

## build-linux: 交叉编译 Linux 版本
build-linux:
	@echo "Building $(BINARY_NAME) for Linux..."
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME)-linux-amd64 ./$(CMD_DIR)
	@echo "Build complete: $(BIN_DIR)/$(BINARY_NAME)-linux-amd64"

## build-race: 编译带竞态检测的版本
build-race:
	@echo "Building $(BINARY_NAME) with race detector..."
	@mkdir -p $(BIN_DIR)
	$(GOBUILD) -race $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME)-race ./$(CMD_DIR)

## test: 运行测试
test:
	@echo "Running tests..."
	$(GOTEST) -v -cover ./...

## test-race: 运行测试（带竞态检测）
test-race:
	@echo "Running tests with race detector..."
	$(GOTEST) -v -race ./...

## bench: 运行基准测试
bench:
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem ./pkg/...

## bench-cpu: 运行基准测试并生成 CPU profile
bench-cpu:
	@echo "Running benchmarks with CPU profiling..."
	$(GOTEST) -bench=. -benchmem -cpuprofile=cpu.prof ./pkg/engine
	@echo "Profile saved to cpu.prof. Use 'go tool pprof cpu.prof' to analyze."

## bench-mem: 运行基准测试并生成内存 profile
bench-mem:
	@echo "Running benchmarks with memory profiling..."
	$(GOTEST) -bench=. -benchmem -memprofile=mem.prof ./pkg/engine
	@echo "Profile saved to mem.prof. Use 'go tool pprof mem.prof' to analyze."

## coverage: 生成测试覆盖率报告
coverage:
	@echo "Generating coverage report..."
	$(GOTEST) -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## lint: 代码检查
lint:
	@echo "Running linter..."
	$(GOVET) ./...
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed, skipping..."; \
	fi

## fmt: 格式化代码
fmt:
	@echo "Formatting code..."
	$(GOFMT) -s -w .

## mod: 更新依赖
mod:
	@echo "Updating dependencies..."
	$(GOMOD) tidy
	$(GOMOD) download

## proto: 生成 protobuf 代码
proto:
	@echo "Generating protobuf code..."
	@if command -v protoc >/dev/null 2>&1; then \
		protoc --go_out=./api/proto --go_opt=paths=source_relative \
			--go-grpc_out=./api/proto --go-grpc_opt=paths=source_relative \
			-I./api/proto api/proto/*.proto; \
		echo "Protobuf code generated."; \
	else \
		echo "protoc not installed. Please install protobuf compiler."; \
		exit 1; \
	fi

## run: 运行服务
run: build
	@echo "Starting match engine..."
	./$(BIN_DIR)/$(BINARY_NAME) -config config.json

## run-dev: 开发模式运行
run-dev:
	@echo "Starting match engine in development mode..."
	$(GOCMD) run ./$(CMD_DIR) -config config.json

## docker-build: 构建 Docker 镜像
docker-build:
	@echo "Building Docker image..."
	docker build -t $(BINARY_NAME):$(VERSION) .
	docker tag $(BINARY_NAME):$(VERSION) $(BINARY_NAME):latest

## docker-run: 运行 Docker 容器
docker-run:
	@echo "Running Docker container..."
	docker run -d -p 50051:50051 -p 8081:8081 -p 9090:9090 \
		--name $(BINARY_NAME) $(BINARY_NAME):latest

## docker-stop: 停止 Docker 容器
docker-stop:
	@echo "Stopping Docker container..."
	docker stop $(BINARY_NAME) && docker rm $(BINARY_NAME)

## clean: 清理构建产物
clean:
	@echo "Cleaning..."
	rm -rf $(BIN_DIR)
	rm -f coverage.out coverage.html
	rm -f cpu.prof mem.prof
	rm -rf snapshots/

## init-config: 生成默认配置文件
init-config:
	@echo "Generating default config..."
	@echo '{"server":{"grpc_addr":":50051","http_addr":":8080","websocket_addr":":8081"},"engine":{"event_buffer_size":100000,"command_buffer_size":100000,"worker_count":1,"snapshot_interval":1000000000},"nats":{"url":"nats://localhost:4222","subject_prefix":"matchengine","enabled":false},"metrics":{"enabled":true,"addr":":9090","namespace":"matchengine"},"audit":{"enabled":true,"file_path":"audit.log","buffer_size":10000,"flush_size":100,"flush_interval":1000000000},"snapshot":{"enabled":true,"snapshot_dir":"./snapshots","interval":1000000000,"depth":100},"logging":{"level":"info","format":"json","output":"stdout"},"symbols":["BTC-USDT","ETH-USDT","BNB-USDT"]}' > config.json
	@echo "Config file created: config.json"

## help: 显示帮助信息
help:
	@echo "Match Engine - Available targets:"
	@echo ""
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'

# 默认目标
.DEFAULT_GOAL := help

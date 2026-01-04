.PHONY: build run clean test deps init-db

# 默认目标
all: build

# 编译
build:
	@./scripts/build.sh

# 运行
run:
	@go run ./cmd/main.go -f etc/config.yaml

# 清理
clean:
	@rm -rf build/
	@rm -f team-assistant

# 测试
test:
	@go test -v ./...

# 安装依赖
deps:
	@go mod tidy
	@go mod download

# 初始化数据库
init-db:
	@echo "请手动执行: mysql -u root -p < deploy/sql/init.sql"

# 格式化代码
fmt:
	@go fmt ./...
	@go vet ./...

# 生成文档
docs:
	@echo "Generating docs..."
	@which swag > /dev/null || go install github.com/swaggo/swag/cmd/swag@latest
	@swag init -g cmd/main.go -o docs/

# 帮助
help:
	@echo "Team Assistant Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build    - 编译项目"
	@echo "  make run      - 运行项目"
	@echo "  make test     - 运行测试"
	@echo "  make deps     - 安装依赖"
	@echo "  make init-db  - 初始化数据库"
	@echo "  make fmt      - 格式化代码"
	@echo "  make clean    - 清理构建产物"

#!/bin/bash

# Team Assistant 编译脚本

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
OUTPUT_DIR="${PROJECT_DIR}/build"
BINARY_NAME="team-assistant"

cd "$PROJECT_DIR"

# 创建输出目录
mkdir -p "$OUTPUT_DIR"

# 获取版本信息
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')
GO_VERSION=$(go version | awk '{print $3}')

# 编译参数
LDFLAGS="-s -w -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.GoVersion=${GO_VERSION}"

echo "Building Team Assistant..."
echo "  Version: ${VERSION}"
echo "  Build Time: ${BUILD_TIME}"
echo "  Go Version: ${GO_VERSION}"

# 编译
CGO_ENABLED=0 go build -ldflags "${LDFLAGS}" -o "${OUTPUT_DIR}/${BINARY_NAME}" ./cmd/main.go

echo ""
echo "Build complete: ${OUTPUT_DIR}/${BINARY_NAME}"
echo ""

# 复制配置文件
cp "${PROJECT_DIR}/etc/config.yaml" "${OUTPUT_DIR}/config.yaml"
echo "Config copied to: ${OUTPUT_DIR}/config.yaml"

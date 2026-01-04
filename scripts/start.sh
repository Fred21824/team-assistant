#!/bin/bash

# Team Assistant 启动脚本

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
CONFIG_FILE="${PROJECT_DIR}/etc/config.yaml"
BINARY_NAME="team-assistant"

cd "$PROJECT_DIR"

# 编译
echo "Building..."
go build -o "${BINARY_NAME}" ./cmd/main.go

# 启动
echo "Starting Team Assistant..."
./"${BINARY_NAME}" -f "${CONFIG_FILE}"

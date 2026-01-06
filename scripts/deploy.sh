#!/bin/bash

# Team Assistant 部署脚本
# 部署到 bsicryptopay 服务器 (18.166.182.134)

set -e

# 获取脚本所在目录
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")

# 配置 - 使用 bsicryptopay 的 SSH 配置
SSH_KEY="/Users/xxxxxx/Work/BSI/bsicryptopay/deploy-key.pem"
SERVER_USER="ubuntu"
SERVER_HOST="ec2-18-166-182-134.ap-east-1.compute.amazonaws.com"
SERVER_IP="18.166.182.134"
SERVER_DIR="/opt/team-assistant"
SERVICE_NAME="team-assistant"
SERVICE_PORT="8090"

# 颜色
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# SSH 命令封装
ssh_cmd() {
    ssh -i "$SSH_KEY" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR "$SERVER_USER@$SERVER_HOST" "$@"
}

scp_cmd() {
    scp -i "$SSH_KEY" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR "$@"
}

cd "$PROJECT_DIR"

# 检查 SSH 连接
check_ssh() {
    log_info "检查 SSH 连接..."

    # 检查密钥文件
    if [ ! -f "$SSH_KEY" ]; then
        log_error "SSH 密钥文件不存在: $SSH_KEY"
        exit 1
    fi

    if ! ssh_cmd "echo 'SSH OK'" &>/dev/null; then
        log_error "无法连接到服务器 ${SERVER_HOST}"
        exit 1
    fi
    log_info "SSH 连接正常"
}

# 编译项目
build() {
    log_info "编译项目 (Linux amd64)..."

    mkdir -p build

    # 使用 homebrew 的 go 避免版本冲突
    GO_CMD="go"
    if [ -x "/opt/homebrew/bin/go" ]; then
        GO_CMD="/opt/homebrew/bin/go"
    fi

    # 编译主服务
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $GO_CMD build -o build/team-assistant ./cmd/main.go
    if [ ! -f "build/team-assistant" ]; then
        log_error "编译主服务失败"
        exit 1
    fi
    log_info "编译完成: build/team-assistant ($(du -h build/team-assistant | cut -f1))"

    # 编译 syncworker
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $GO_CMD build -o build/syncworker ./cmd/syncworker/main.go
    if [ ! -f "build/syncworker" ]; then
        log_error "编译 syncworker 失败"
        exit 1
    fi
    log_info "编译完成: build/syncworker ($(du -h build/syncworker | cut -f1))"

    # 编译 reindex
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $GO_CMD build -o build/reindex ./cmd/reindex/main.go
    if [ ! -f "build/reindex" ]; then
        log_error "编译 reindex 失败"
        exit 1
    fi
    log_info "编译完成: build/reindex ($(du -h build/reindex | cut -f1))"
}

# 初始化数据库
init_db() {
    log_info "初始化数据库..."

    # 先上传 SQL 文件
    log_info "上传 SQL 脚本..."
    scp_cmd deploy/sql/init.sql "${SERVER_USER}@${SERVER_HOST}:/tmp/init.sql"

    # 执行 SQL
    log_info "执行数据库初始化..."
    ssh_cmd << 'ENDSSH'
mysql -u root -p123456 < /tmp/init.sql
rm /tmp/init.sql
ENDSSH

    log_info "数据库初始化完成"
}

# 部署到服务器（包含配置文件）
deploy() {
    log_info "部署到服务器..."

    # 创建目录
    ssh_cmd "sudo mkdir -p ${SERVER_DIR}/etc ${SERVER_DIR}/logs ${SERVER_DIR}/bin && sudo chown -R ubuntu:ubuntu ${SERVER_DIR}"

    # 上传主服务
    log_info "上传主服务..."
    scp_cmd build/team-assistant "${SERVER_USER}@${SERVER_HOST}:/tmp/"
    ssh_cmd "sudo mv /tmp/team-assistant ${SERVER_DIR}/ && sudo chmod +x ${SERVER_DIR}/team-assistant"

    # 上传 syncworker
    log_info "上传 syncworker..."
    scp_cmd build/syncworker "${SERVER_USER}@${SERVER_HOST}:/tmp/"
    ssh_cmd "sudo mv /tmp/syncworker ${SERVER_DIR}/bin/ && sudo chmod +x ${SERVER_DIR}/bin/syncworker"

    # 上传 reindex
    log_info "上传 reindex..."
    scp_cmd build/reindex "${SERVER_USER}@${SERVER_HOST}:/tmp/"
    ssh_cmd "sudo mv /tmp/reindex ${SERVER_DIR}/bin/ && sudo chmod +x ${SERVER_DIR}/bin/reindex"

    # 上传配置文件（使用服务器专用配置）
    log_info "上传配置文件..."
    if [ -f "etc/config.server.yaml" ]; then
        scp_cmd etc/config.server.yaml "${SERVER_USER}@${SERVER_HOST}:/tmp/config.yaml"
    else
        scp_cmd etc/config.yaml "${SERVER_USER}@${SERVER_HOST}:/tmp/config.yaml"
    fi
    ssh_cmd "sudo mv /tmp/config.yaml ${SERVER_DIR}/etc/"

    log_info "部署完成"
}

# 仅部署二进制文件（不覆盖配置）
deploy_binary_only() {
    log_info "部署二进制文件（保留服务器配置）..."

    # 创建目录
    ssh_cmd "sudo mkdir -p ${SERVER_DIR}/etc ${SERVER_DIR}/logs ${SERVER_DIR}/bin && sudo chown -R ubuntu:ubuntu ${SERVER_DIR}"

    # 上传主服务
    log_info "上传主服务..."
    scp_cmd build/team-assistant "${SERVER_USER}@${SERVER_HOST}:/tmp/"
    ssh_cmd "sudo mv /tmp/team-assistant ${SERVER_DIR}/ && sudo chmod +x ${SERVER_DIR}/team-assistant"

    # 上传 syncworker
    log_info "上传 syncworker..."
    scp_cmd build/syncworker "${SERVER_USER}@${SERVER_HOST}:/tmp/"
    ssh_cmd "sudo mv /tmp/syncworker ${SERVER_DIR}/bin/ && sudo chmod +x ${SERVER_DIR}/bin/syncworker"

    # 上传 reindex
    log_info "上传 reindex..."
    scp_cmd build/reindex "${SERVER_USER}@${SERVER_HOST}:/tmp/"
    ssh_cmd "sudo mv /tmp/reindex ${SERVER_DIR}/bin/ && sudo chmod +x ${SERVER_DIR}/bin/reindex"

    log_info "部署完成（配置文件未修改）"
}

# 创建 systemd 服务
install_service() {
    log_info "安装 systemd 服务..."

    # 主服务
    ssh_cmd << ENDSSH
sudo tee /etc/systemd/system/${SERVICE_NAME}.service > /dev/null << 'EOF'
[Unit]
Description=Team Assistant - AI-powered Team Collaboration Bot
After=network.target mysql.service redis.service

[Service]
Type=simple
User=ubuntu
WorkingDirectory=${SERVER_DIR}
ExecStart=${SERVER_DIR}/team-assistant -f ${SERVER_DIR}/etc/config.yaml
Restart=always
RestartSec=5
StandardOutput=append:${SERVER_DIR}/logs/stdout.log
StandardError=append:${SERVER_DIR}/logs/stderr.log

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable ${SERVICE_NAME}
ENDSSH

    # syncworker 服务
    ssh_cmd << ENDSSH
sudo tee /etc/systemd/system/syncworker.service > /dev/null << 'EOF'
[Unit]
Description=Team Assistant Sync Worker - Message Sync Service
After=network.target mysql.service redis.service team-assistant.service

[Service]
Type=simple
User=ubuntu
WorkingDirectory=${SERVER_DIR}
ExecStart=${SERVER_DIR}/bin/syncworker -f ${SERVER_DIR}/etc/config.yaml
Restart=always
RestartSec=10
StandardOutput=append:${SERVER_DIR}/logs/syncworker.log
StandardError=append:${SERVER_DIR}/logs/syncworker.log

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable syncworker
ENDSSH

    log_info "服务安装完成"
}

# 启动服务
start() {
    log_info "启动主服务..."
    ssh_cmd "sudo systemctl start ${SERVICE_NAME}"
    sleep 2

    log_info "启动 syncworker..."
    ssh_cmd "sudo systemctl start syncworker"
    sleep 2

    # 检查状态
    local main_ok=false
    local sync_ok=false

    if ssh_cmd "sudo systemctl is-active ${SERVICE_NAME}" | grep -q "active"; then
        main_ok=true
        log_info "主服务启动成功"
    else
        log_error "主服务启动失败"
        ssh_cmd "sudo journalctl -u ${SERVICE_NAME} -n 20 --no-pager"
    fi

    if ssh_cmd "sudo systemctl is-active syncworker" | grep -q "active"; then
        sync_ok=true
        log_info "syncworker 启动成功"
    else
        log_warn "syncworker 启动失败（非致命）"
        ssh_cmd "sudo journalctl -u syncworker -n 20 --no-pager"
    fi

    if [ "$main_ok" = true ]; then
        log_info "访问地址: http://${SERVER_IP}:${SERVICE_PORT}"
        log_info "健康检查: http://${SERVER_IP}:${SERVICE_PORT}/health"
        log_info "飞书 Webhook: http://${SERVER_IP}:${SERVICE_PORT}/webhook/lark"
    else
        exit 1
    fi
}

# 停止服务
stop() {
    log_info "停止服务..."
    ssh_cmd "sudo systemctl stop syncworker 2>/dev/null || true"
    ssh_cmd "sudo systemctl stop ${SERVICE_NAME} 2>/dev/null || true"
    log_info "所有服务已停止"
}

# 重启服务
restart() {
    log_info "重启服务..."
    ssh_cmd "sudo systemctl restart ${SERVICE_NAME}"
    ssh_cmd "sudo systemctl restart syncworker"
    sleep 3

    if ssh_cmd "sudo systemctl is-active ${SERVICE_NAME}" | grep -q "active"; then
        log_info "主服务重启成功"
    else
        log_error "主服务重启失败"
        ssh_cmd "sudo journalctl -u ${SERVICE_NAME} -n 30 --no-pager"
        exit 1
    fi

    if ssh_cmd "sudo systemctl is-active syncworker" | grep -q "active"; then
        log_info "syncworker 重启成功"
    else
        log_warn "syncworker 重启失败"
    fi
}

# 查看状态
status() {
    log_info "=== 主服务状态 ==="
    ssh_cmd "sudo systemctl status ${SERVICE_NAME} --no-pager || true"
    echo ""
    log_info "=== syncworker 状态 ==="
    ssh_cmd "sudo systemctl status syncworker --no-pager || true"
}

# 查看日志
logs() {
    ssh_cmd "sudo journalctl -u ${SERVICE_NAME} -f"
}

# 完整部署流程
full_deploy() {
    check_ssh
    build
    init_db
    stop
    deploy
    install_service
    start

    echo ""
    log_info "=========================================="
    log_info "部署完成！"
    log_info "=========================================="
    echo ""
    log_info "服务信息:"
    log_info "  - 服务地址: http://${SERVER_IP}:${SERVICE_PORT}"
    log_info "  - 健康检查: curl http://${SERVER_IP}:${SERVICE_PORT}/health"
    log_info "  - 飞书 Webhook: http://${SERVER_IP}:${SERVICE_PORT}/webhook/lark"
    log_info "  - GitHub Webhook: http://${SERVER_IP}:${SERVICE_PORT}/webhook/github"
    echo ""
    log_info "下一步操作:"
    log_info "  1. 在飞书开放平台配置事件订阅 URL: http://${SERVER_IP}:${SERVICE_PORT}/webhook/lark"
    log_info "  2. 将机器人添加到群聊"
    log_info "  3. @机器人 发送 '帮助' 测试"
    echo ""
}

# 仅更新代码（快速部署，不覆盖配置）
quick_deploy() {
    check_ssh
    build
    stop
    deploy_binary_only
    start
}

# 帮助
usage() {
    echo "Usage: $0 {full|quick|start|stop|restart|status|logs|init-db|build}"
    echo ""
    echo "Commands:"
    echo "  full      完整部署（编译+数据库+部署+启动）"
    echo "  quick     快速部署（仅更新代码）"
    echo "  start     启动服务"
    echo "  stop      停止服务"
    echo "  restart   重启服务"
    echo "  status    查看状态"
    echo "  logs      查看日志"
    echo "  init-db   初始化数据库"
    echo "  build     仅编译"
}

# 主流程
case "${1:-full}" in
    full)
        full_deploy
        ;;
    quick)
        quick_deploy
        ;;
    start)
        check_ssh
        start
        ;;
    stop)
        check_ssh
        stop
        ;;
    restart)
        check_ssh
        restart
        ;;
    status)
        check_ssh
        status
        ;;
    logs)
        check_ssh
        logs
        ;;
    init-db)
        check_ssh
        init_db
        ;;
    build)
        build
        ;;
    *)
        usage
        exit 1
        ;;
esac

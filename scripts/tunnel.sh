#!/bin/bash

# Team Assistant 内网穿透脚本
# 用法: ./scripts/tunnel.sh start|stop|status|restart

PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PID_FILE="$PROJECT_DIR/.tunnel.pid"
SERVICE_PID_FILE="$PROJECT_DIR/.service.pid"
LOG_FILE="$PROJECT_DIR/logs/tunnel.log"
SERVICE_LOG_FILE="$PROJECT_DIR/logs/service.log"
PORT=8090

# 确保 logs 目录存在
mkdir -p "$PROJECT_DIR/logs"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 检查服务是否运行
check_service() {
    if [ -f "$SERVICE_PID_FILE" ]; then
        pid=$(cat "$SERVICE_PID_FILE")
        if ps -p "$pid" > /dev/null 2>&1; then
            return 0
        fi
    fi
    return 1
}

# 检查隧道是否运行
check_tunnel() {
    if [ -f "$PID_FILE" ]; then
        pid=$(cat "$PID_FILE")
        if ps -p "$pid" > /dev/null 2>&1; then
            return 0
        fi
    fi
    return 1
}

# 启动服务
start_service() {
    if check_service; then
        log_warn "服务已在运行 (PID: $(cat $SERVICE_PID_FILE))"
        return 0
    fi

    log_info "编译项目..."
    cd "$PROJECT_DIR"
    go build -o team-assistant ./cmd/main.go
    if [ $? -ne 0 ]; then
        log_error "编译失败"
        return 1
    fi

    log_info "启动服务..."
    nohup ./team-assistant -f etc/config.yaml > "$SERVICE_LOG_FILE" 2>&1 &
    echo $! > "$SERVICE_PID_FILE"

    # 等待服务启动
    sleep 2

    # 检查服务是否成功启动
    if curl -s "http://localhost:$PORT/health" > /dev/null 2>&1; then
        log_info "服务启动成功 (PID: $(cat $SERVICE_PID_FILE))"
        return 0
    else
        log_error "服务启动失败，请检查日志: $SERVICE_LOG_FILE"
        return 1
    fi
}

# 停止服务
stop_service() {
    if ! check_service; then
        log_warn "服务未运行"
        return 0
    fi

    pid=$(cat "$SERVICE_PID_FILE")
    log_info "停止服务 (PID: $pid)..."
    kill "$pid" 2>/dev/null
    rm -f "$SERVICE_PID_FILE"
    log_info "服务已停止"
}

# 启动隧道
start_tunnel() {
    # 先确保服务在运行
    if ! check_service; then
        log_info "服务未运行，先启动服务..."
        start_service
        if [ $? -ne 0 ]; then
            log_error "服务启动失败，无法创建隧道"
            return 1
        fi
    fi

    if check_tunnel; then
        log_warn "隧道已在运行 (PID: $(cat $PID_FILE))"
        show_url
        return 0
    fi

    log_info "启动 cloudflared 隧道..."
    nohup cloudflared tunnel --url "http://localhost:$PORT" > "$LOG_FILE" 2>&1 &
    echo $! > "$PID_FILE"

    # 等待隧道建立
    log_info "等待隧道建立..."
    sleep 5

    show_url
}

# 显示隧道 URL
show_url() {
    if [ -f "$LOG_FILE" ]; then
        url=$(grep -o 'https://[a-z0-9-]*\.trycloudflare\.com' "$LOG_FILE" | head -1)
        if [ -n "$url" ]; then
            echo ""
            log_info "============================================"
            log_info "隧道 URL: $url"
            log_info "飞书 Webhook: $url/webhook/lark"
            log_info "GitHub Webhook: $url/webhook/github"
            log_info "健康检查: $url/health"
            log_info "============================================"
            echo ""
        else
            log_warn "未找到隧道 URL，请稍后重试或检查日志: $LOG_FILE"
        fi
    fi
}

# 停止隧道
stop_tunnel() {
    if ! check_tunnel; then
        log_warn "隧道未运行"
        return 0
    fi

    pid=$(cat "$PID_FILE")
    log_info "停止隧道 (PID: $pid)..."
    kill "$pid" 2>/dev/null
    rm -f "$PID_FILE"
    log_info "隧道已停止"
}

# 显示状态
show_status() {
    echo ""
    echo "========== Team Assistant 状态 =========="

    if check_service; then
        log_info "服务: 运行中 (PID: $(cat $SERVICE_PID_FILE))"
    else
        log_warn "服务: 未运行"
    fi

    if check_tunnel; then
        log_info "隧道: 运行中 (PID: $(cat $PID_FILE))"
        show_url
    else
        log_warn "隧道: 未运行"
    fi

    echo "=========================================="
    echo ""
}

# 全部启动
start_all() {
    start_service
    if [ $? -eq 0 ]; then
        start_tunnel
    fi
}

# 全部停止
stop_all() {
    stop_tunnel
    stop_service
}

# 重启
restart_all() {
    stop_all
    sleep 2
    start_all
}

# 主函数
case "$1" in
    start)
        start_all
        ;;
    stop)
        stop_all
        ;;
    restart)
        restart_all
        ;;
    status)
        show_status
        ;;
    service-start)
        start_service
        ;;
    service-stop)
        stop_service
        ;;
    tunnel-start)
        start_tunnel
        ;;
    tunnel-stop)
        stop_tunnel
        ;;
    url)
        show_url
        ;;
    logs)
        echo "=== 服务日志 (最近 20 行) ==="
        tail -20 "$SERVICE_LOG_FILE" 2>/dev/null || echo "无日志"
        echo ""
        echo "=== 隧道日志 (最近 20 行) ==="
        tail -20 "$LOG_FILE" 2>/dev/null || echo "无日志"
        ;;
    *)
        echo "用法: $0 {start|stop|restart|status|url|logs}"
        echo ""
        echo "命令:"
        echo "  start    - 启动服务和隧道"
        echo "  stop     - 停止服务和隧道"
        echo "  restart  - 重启服务和隧道"
        echo "  status   - 查看状态"
        echo "  url      - 显示隧道 URL"
        echo "  logs     - 查看日志"
        echo ""
        echo "高级命令:"
        echo "  service-start  - 仅启动服务"
        echo "  service-stop   - 仅停止服务"
        echo "  tunnel-start   - 仅启动隧道"
        echo "  tunnel-stop    - 仅停止隧道"
        exit 1
        ;;
esac

exit 0

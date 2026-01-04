#!/bin/bash

# Dify 部署脚本
# 使用方法: ./scripts/deploy-dify.sh [start|stop|status|logs]

set -e

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
DIFY_DIR="$PROJECT_DIR/deploy/dify"

cd "$DIFY_DIR"

case "${1:-start}" in
    start)
        echo "🚀 Starting Dify..."
        docker compose up -d
        echo ""
        echo "✅ Dify started successfully!"
        echo ""
        echo "📝 Access URLs:"
        echo "   - Web Console: http://localhost:3000"
        echo "   - API Endpoint: http://localhost/v1"
        echo ""
        echo "🔑 Next steps:"
        echo "   1. Open http://localhost:3000 in your browser"
        echo "   2. Create an admin account"
        echo "   3. Create a new 'Chat' application"
        echo "   4. Get the API Key from Settings > API Access"
        echo "   5. Update team-assistant config with the API Key"
        echo ""
        ;;
    stop)
        echo "🛑 Stopping Dify..."
        docker compose down
        echo "✅ Dify stopped"
        ;;
    status)
        echo "📊 Dify Status:"
        docker compose ps
        ;;
    logs)
        docker compose logs -f ${2:-}
        ;;
    restart)
        echo "🔄 Restarting Dify..."
        docker compose restart
        echo "✅ Dify restarted"
        ;;
    clean)
        echo "🧹 Cleaning Dify (removing volumes)..."
        docker compose down -v
        echo "✅ Dify cleaned"
        ;;
    *)
        echo "Usage: $0 {start|stop|status|logs|restart|clean}"
        exit 1
        ;;
esac

#!/bin/bash

# NATS Multi-Agent 测试脚本
# 用于快速启动 NATS 服务器并运行示例

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

# 颜色输出
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== NATS Multi-Agent Communication Test ===${NC}"
echo ""

# 检查 Docker 是否安装
if ! command -v docker &> /dev/null; then
    echo -e "${RED}Error: Docker is not installed${NC}"
    echo "Please install Docker first: https://docs.docker.com/get-docker/"
    exit 1
fi

# 检查 docker-compose 是否安装
if ! command -v docker-compose &> /dev/null; then
    echo -e "${YELLOW}Warning: docker-compose not found, trying 'docker compose'${NC}"
    DOCKER_COMPOSE="docker compose"
else
    DOCKER_COMPOSE="docker-compose"
fi

# 函数: 启动 NATS
start_nats() {
    echo -e "${GREEN}Starting NATS server...${NC}"
    $DOCKER_COMPOSE up -d nats

    echo -e "${YELLOW}Waiting for NATS to be ready...${NC}"
    sleep 3

    # 检查 NATS 健康状态
    for i in {1..10}; do
        if docker exec goagent-nats nats-server --version &> /dev/null; then
            echo -e "${GREEN}✓ NATS server is ready!${NC}"
            return 0
        fi
        echo -n "."
        sleep 1
    done

    echo -e "${RED}Failed to start NATS server${NC}"
    return 1
}

# 函数: 停止 NATS
stop_nats() {
    echo -e "${YELLOW}Stopping NATS server...${NC}"
    $DOCKER_COMPOSE down
    echo -e "${GREEN}✓ NATS server stopped${NC}"
}

# 函数: 查看 NATS 日志
show_logs() {
    echo -e "${GREEN}Showing NATS logs (Ctrl+C to exit):${NC}"
    $DOCKER_COMPOSE logs -f nats
}

# 函数: NATS 状态
status() {
    echo -e "${GREEN}NATS Server Status:${NC}"
    $DOCKER_COMPOSE ps
    echo ""

    if docker ps | grep -q goagent-nats; then
        echo -e "${GREEN}✓ NATS is running${NC}"
        echo ""
        echo "Monitoring URLs:"
        echo "  - HTTP Management: http://localhost:8222"
        echo "  - Metrics Exporter: http://localhost:7777/metrics"
        echo ""
        echo "Connection:"
        echo "  - NATS URL: nats://localhost:4222"
    else
        echo -e "${RED}✗ NATS is not running${NC}"
    fi
}

# 函数: 运行示例
run_example() {
    echo -e "${GREEN}Running Multi-Agent NATS Example...${NC}"
    echo ""

    # 检查 NATS 是否运行
    if ! docker ps | grep -q goagent-nats; then
        echo -e "${YELLOW}NATS is not running. Starting it first...${NC}"
        start_nats
        echo ""
    fi

    # 运行 Go 程序（基础模式）
    cd "$SCRIPT_DIR"
    NATS_URL=nats://localhost:4222 go run .
}

# 函数: 运行 AI 协作示例
run_ai() {
    echo -e "${GREEN}Running AI-Powered Multi-Agent Collaboration...${NC}"
    echo ""

    # 检查 API Key
    if [ -z "$DEEPSEEK_API_KEY" ]; then
        echo -e "${RED}Error: DEEPSEEK_API_KEY environment variable is required${NC}"
        echo ""
        echo "Please set your DeepSeek API key:"
        echo "  export DEEPSEEK_API_KEY='your-api-key-here'"
        echo ""
        echo "Get your API key from: https://platform.deepseek.com/"
        exit 1
    fi

    # 检查 NATS 是否运行
    if ! docker ps | grep -q goagent-nats; then
        echo -e "${YELLOW}NATS is not running. Starting it first...${NC}"
        start_nats
        echo ""
    fi

    # 运行 AI 模式
    cd "$SCRIPT_DIR"
    MODE=ai NATS_URL=nats://localhost:4222 DEEPSEEK_API_KEY="$DEEPSEEK_API_KEY" go run .
}

# 函数: 运行完整测试流程
test_full() {
    echo -e "${GREEN}Running full test workflow...${NC}"
    echo ""

    # 启动 NATS
    start_nats
    echo ""

    # 显示状态
    status
    echo ""

    # 运行示例
    run_example

    echo ""
    echo -e "${GREEN}Test completed!${NC}"
    echo ""
    echo "To view NATS logs: $0 logs"
    echo "To stop NATS: $0 stop"
}

# 函数: 显示帮助
show_help() {
    cat << EOF
NATS Multi-Agent Test Script

Usage: $0 [command]

Commands:
    start       Start NATS server using Docker
    stop        Stop NATS server
    restart     Restart NATS server
    status      Show NATS server status
    logs        Show NATS server logs (follow mode)
    run         Run the basic multi-agent example
    ai          Run AI-powered collaboration example (requires DEEPSEEK_API_KEY)
    test        Run full test (start NATS + run basic example)
    clean       Stop and remove all containers and volumes
    help        Show this help message

Examples:
    # Quick test (recommended for first time)
    $0 test

    # Run AI collaboration (set API key first)
    export DEEPSEEK_API_KEY='your-key-here'
    $0 ai

    # Manual workflow
    $0 start
    $0 run
    $0 logs
    $0 stop

    # Check if NATS is running
    $0 status

Environment Variables:
    NATS_URL           NATS server URL (default: nats://localhost:4222)
    DEEPSEEK_API_KEY   DeepSeek API key (required for AI mode)

EOF
}

# 函数: 清理
clean() {
    echo -e "${YELLOW}Cleaning up...${NC}"
    $DOCKER_COMPOSE down -v
    echo -e "${GREEN}✓ Cleanup completed${NC}"
}

# 主命令处理
case "${1:-test}" in
    start)
        start_nats
        ;;
    stop)
        stop_nats
        ;;
    restart)
        stop_nats
        echo ""
        start_nats
        ;;
    status)
        status
        ;;
    logs)
        show_logs
        ;;
    run)
        run_example
        ;;
    ai)
        run_ai
        ;;
    test)
        test_full
        ;;
    clean)
        clean
        ;;
    help|--help|-h)
        show_help
        ;;
    *)
        echo -e "${RED}Unknown command: $1${NC}"
        echo ""
        show_help
        exit 1
        ;;
esac

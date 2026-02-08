#!/bin/bash
# AI 法律助手 - 一键启动脚本
# 用法: ./start.sh [api-key]
#   或: export DASHSCOPE_API_KEY="sk-xxx" && ./start.sh

set -e

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$PROJECT_DIR"

echo ""
echo -e "${CYAN}╔═══════════════════════════════════════╗${NC}"
echo -e "${CYAN}║        AI 法律助手 - 启动脚本         ║${NC}"
echo -e "${CYAN}╚═══════════════════════════════════════╝${NC}"
echo ""

# ── 1. 检查 API Key ──
if [ -n "$1" ]; then
    export DASHSCOPE_API_KEY="$1"
fi

if [ -z "$DASHSCOPE_API_KEY" ] && [ -z "$QWEN_API_KEY" ]; then
    echo -e "${RED}错误: 未设置 API Key${NC}"
    echo ""
    echo "请通过以下方式之一设置:"
    echo "  1. ./start.sh sk-your-api-key-here"
    echo "  2. export DASHSCOPE_API_KEY=\"sk-xxx\" && ./start.sh"
    echo ""
    echo -e "获取 API Key: ${CYAN}https://bailian.console.aliyun.com/${NC}"
    exit 1
fi
echo -e "${GREEN}✓${NC} API Key 已配置"

# ── 2. 检查 Go ──
if ! command -v go &> /dev/null; then
    echo -e "${RED}错误: 未找到 Go，请先安装 Go 1.21+${NC}"
    exit 1
fi
echo -e "${GREEN}✓${NC} Go $(go version | awk '{print $3}')"

# ── 3. 检查 Node.js ──
if ! command -v node &> /dev/null; then
    echo -e "${RED}错误: 未找到 Node.js，请先安装 Node.js 18+${NC}"
    exit 1
fi
echo -e "${GREEN}✓${NC} Node $(node --version)"

# ── 4. 安装后端依赖 ──
echo ""
echo -e "${YELLOW}▸ 安装后端依赖...${NC}"
go mod tidy
echo -e "${GREEN}✓${NC} 后端依赖就绪"

# ── 5. 安装前端依赖 ──
echo -e "${YELLOW}▸ 安装前端依赖...${NC}"
cd web
if [ ! -d "node_modules" ]; then
    npm install --silent
fi
cd "$PROJECT_DIR"
echo -e "${GREEN}✓${NC} 前端依赖就绪"

# ── 6. 创建上传目录 ──
mkdir -p uploads

# ── 7. 启动后端 ──
SERVER_PORT="${SERVER_PORT:-8080}"
echo ""
echo -e "${YELLOW}▸ 启动后端服务 (端口 ${SERVER_PORT})...${NC}"
go run ./cmd/server/ &
BACKEND_PID=$!
echo -e "${GREEN}✓${NC} 后端 PID: $BACKEND_PID"

# 等后端启动
sleep 3

# ── 8. 启动前端 ──
echo -e "${YELLOW}▸ 启动前端开发服务器 (端口 5173)...${NC}"
cd web
npx vite --host &
FRONTEND_PID=$!
cd "$PROJECT_DIR"
echo -e "${GREEN}✓${NC} 前端 PID: $FRONTEND_PID"

# ── 9. 完成 ──
sleep 2
echo ""
echo -e "${CYAN}═══════════════════════════════════════${NC}"
echo -e "${GREEN}  AI 法律助手已启动!${NC}"
echo ""
echo -e "  前端界面: ${CYAN}http://localhost:5173${NC}"
echo -e "  后端 API: ${CYAN}http://localhost:${SERVER_PORT}${NC}"
echo ""
echo -e "  按 ${YELLOW}Ctrl+C${NC} 停止所有服务"
echo -e "${CYAN}═══════════════════════════════════════${NC}"
echo ""

# ── 捕获退出信号，清理子进程 ──
cleanup() {
    echo ""
    echo -e "${YELLOW}正在停止服务...${NC}"
    kill $BACKEND_PID 2>/dev/null || true
    kill $FRONTEND_PID 2>/dev/null || true
    wait $BACKEND_PID 2>/dev/null || true
    wait $FRONTEND_PID 2>/dev/null || true
    echo -e "${GREEN}✓${NC} 所有服务已停止"
    exit 0
}

trap cleanup SIGINT SIGTERM

# 等待子进程
wait

.PHONY: all build run dev stop clean install install-backend install-frontend \
       build-backend build-frontend run-backend run-frontend dev-backend dev-frontend \
       check help

# ========== 默认目标 ==========
help: ## 显示帮助信息
	@echo ""
	@echo "AI 法律助手 - 可用命令："
	@echo "────────────────────────────────────────"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  make %-18s %s\n", $$1, $$2}'
	@echo ""

# ========== 一键操作 ==========
all: install build ## 安装依赖并编译全部

install: install-backend install-frontend ## 安装全部依赖

build: build-backend build-frontend ## 编译全部

dev: check ## 同时启动前后端开发服务器
	@echo "🚀 启动 AI 法律助手开发环境..."
	@echo "   后端: http://localhost:$${SERVER_PORT:-8080}"
	@echo "   前端: http://localhost:5173"
	@echo ""
	@$(MAKE) dev-backend &
	@sleep 2
	@$(MAKE) dev-frontend

run: check build-backend ## 编译并运行后端 + 前端开发服务器
	@echo "🚀 启动 AI 法律助手..."
	@$(MAKE) run-backend &
	@sleep 2
	@$(MAKE) dev-frontend

stop: ## 停止所有服务
	@echo "⏹  停止服务..."
	@-pkill -f "law-assistant" 2>/dev/null || true
	@-pkill -f "vite" 2>/dev/null || true
	@echo "   已停止"

# ========== 后端 ==========
install-backend: ## 安装后端 Go 依赖
	@echo "📦 安装后端依赖..."
	go mod tidy
	@echo "   ✓ 后端依赖安装完成"

build-backend: ## 编译后端
	@echo "🔨 编译后端..."
	go build -o bin/law-assistant ./cmd/server/
	@echo "   ✓ 编译完成: bin/law-assistant"

run-backend: check ## 运行编译后的后端
	@echo "🖥  启动后端服务 (端口 $${SERVER_PORT:-8080})..."
	./bin/law-assistant

dev-backend: check ## 开发模式运行后端（直接 go run）
	@echo "🖥  启动后端开发服务器 (端口 $${SERVER_PORT:-8080})..."
	go run ./cmd/server/

# ========== 前端 ==========
install-frontend: ## 安装前端 npm 依赖
	@echo "📦 安装前端依赖..."
	cd web && npm install
	@echo "   ✓ 前端依赖安装完成"

build-frontend: ## 编译前端（生产构建）
	@echo "🔨 编译前端..."
	cd web && npx vite build
	@echo "   ✓ 前端构建完成: web/dist/"

dev-frontend: ## 启动前端开发服务器
	@echo "🌐 启动前端开发服务器 (端口 5173)..."
	cd web && npx vite

# ========== 工具 ==========
check: ## 检查环境变量是否已设置
	@if [ -z "$${DASHSCOPE_API_KEY}" ] && [ -z "$${QWEN_API_KEY}" ]; then \
		echo ""; \
		echo "❌ 错误: 未设置 API Key 环境变量"; \
		echo ""; \
		echo "请先设置百炼平台 API Key:"; \
		echo "  export DASHSCOPE_API_KEY=\"sk-xxxxxx\""; \
		echo ""; \
		echo "获取地址: https://bailian.console.aliyun.com/"; \
		echo ""; \
		exit 1; \
	fi
	@echo "✓ API Key 已配置"

clean: ## 清理编译产物
	@echo "🧹 清理编译产物..."
	rm -rf bin/
	rm -rf web/dist/
	rm -rf web/node_modules/
	@echo "   ✓ 清理完成"

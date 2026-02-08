# AI 法律助手

基于 Go [Eino](https://github.com/cloudwego/eino) 框架和通义千问大模型构建的智能法律服务平台。

## 功能模块

- **法律咨询** - 多轮对话式法律问答，覆盖民事/刑事/行政/劳动法等领域
- **诉状撰写** - 自动生成起诉状/答辩状/上诉状等规范法律文书
- **合同优化** - 上传 Word/PDF 合同文件，审查风险条款并生成修改建议
- **证据整理** - 对证据材料进行分类、排序，生成规范证据清单
- **取证指导** - 根据案件类型指导证据收集方向和注意事项
- **沟通话术** - 生成律师与客户/当事人的沟通策略和话术模板

## 技术栈

| 层级 | 技术 |
|------|------|
| 后端框架 | Go + [Eino](https://github.com/cloudwego/eino) |
| 大模型 | 通义千问 (qwen-max) via OpenAI 兼容 API |
| 前端 | React 18 + TypeScript + Tailwind CSS |
| 通信 | SSE (Server-Sent Events) 流式响应 |

## 快速开始

### 前置条件

- Go 1.21+
- Node.js 18+
- 阿里云百炼 API Key（[获取地址](https://bailian.console.aliyun.com/)，进入控制台 -> API-KEY 管理 -> 创建 API Key）

### 方式一：一键启动脚本（推荐）

```bash
# 直接传入 API Key
./start.sh sk-your-api-key-here

# 或先设置环境变量再启动
export DASHSCOPE_API_KEY="sk-xxxxxxxxxxxxxxxxxxxxxxxx"
./start.sh
```

脚本会自动安装依赖、启动后端和前端，按 `Ctrl+C` 一键停止所有服务。

### 方式二：使用 Makefile

```bash
export DASHSCOPE_API_KEY="sk-xxxxxxxxxxxxxxxxxxxxxxxx"

make install    # 安装全部依赖（后端 + 前端）
make dev        # 同时启动前后端开发服务器
make stop       # 停止所有服务
```

更多命令：

```bash
make help             # 查看所有可用命令
make build            # 编译全部（后端二进制 + 前端静态文件）
make run              # 编译后运行
make dev-backend      # 仅启动后端
make dev-frontend     # 仅启动前端
make clean            # 清理编译产物
```

### 方式三：手动分步启动

```bash
# 1. 设置环境变量
export DASHSCOPE_API_KEY="sk-xxxxxxxxxxxxxxxxxxxxxxxx"

# 2. 启动后端
go mod tidy
go run cmd/server/main.go

# 3. 新开终端，启动前端
cd web && npm install && npm run dev
```

前端默认运行在 http://localhost:5173，后端 API 在 http://localhost:8080。

## 项目结构

```
├── cmd/server/main.go          # 程序入口
├── internal/
│   ├── config/                 # 配置管理
│   ├── model/                  # 千问 ChatModel 接入
│   ├── agent/                  # 各功能 Agent 实现
│   ├── prompt/                 # Prompt 模板
│   ├── tool/                   # 文档解析等工具
│   ├── handler/                # HTTP API 处理
│   └── store/                  # 会话和文件存储
├── web/                        # React 前端
├── docs/PRD.md                 # 产品需求文档
└── uploads/                    # 上传文件存储
```

## API 接口

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | /api/chat | 发送消息（SSE 流式响应） |
| POST | /api/upload | 上传文件（Word/PDF） |
| POST | /api/sessions | 创建会话 |
| GET | /api/sessions | 获取会话列表 |
| GET | /api/sessions/:id | 获取会话详情 |
| DELETE | /api/sessions/:id | 删除会话 |
| GET | /api/modules | 获取功能模块列表 |

## 环境变量

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| DASHSCOPE_API_KEY | 是 | - | 百炼平台 API Key（优先读取） |
| QWEN_API_KEY | 否 | - | 备选环境变量名（DASHSCOPE_API_KEY 未设置时读取） |
| QWEN_MODEL | 否 | qwen-max | 模型名称（可选 qwen-plus、qwen-turbo） |
| QWEN_BASE_URL | 否 | https://dashscope.aliyuncs.com/compatible-mode/v1 | API 地址 |
| SERVER_PORT | 否 | 8080 | 服务端口 |
| UPLOAD_DIR | 否 | ./uploads | 文件上传目录 |
| FRONTEND_URL | 否 | http://localhost:5173 | 前端地址（CORS） |

## License

MIT

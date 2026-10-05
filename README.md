# AI 法律助手

基于 Go [Eino](https://github.com/cloudwego/eino) 框架和通义千问大模型构建的智能法律服务平台。

## 功能模块

- **法律咨询** - 多轮对话式法律问答，覆盖民事/刑事/行政/劳动法等领域
- **诉状撰写** - 自动生成起诉状/答辩状/上诉状等规范法律文书
- **合同优化** - 上传 Word/PDF 合同文件，审查风险条款并生成修改建议
- **证据整理** - 对证据材料进行分类、排序，生成规范证据清单
- **取证指导** - 根据案件类型指导证据收集方向和注意事项
- **沟通话术** - 生成律师与客户/当事人的沟通策略和话术模板
- **登录鉴权** - 基于 [Supabase Auth](https://supabase.com/auth)，会话与上传文件按用户隔离
- **部署** - 见 [docs/DEPLOY.md](docs/DEPLOY.md)（Vercel + Docker 后端 + Supabase）

## 技术栈

| 层级 | 技术 |
|------|------|
| 后端框架 | Go + [Eino](https://github.com/cloudwego/eino) |
| 大模型 | 通义千问 (qwen-max) via OpenAI 兼容 API |
| 前端 | React 18 + TypeScript + Tailwind CSS |
| 通信 | SSE (Server-Sent Events) 流式响应 |
| 鉴权 | Supabase Auth（后端校验 JWT，前端 `@supabase/supabase-js`） |

## 快速开始

### 前置条件

- Go 1.24+
- Node.js 20.19+ 或 22.12+（Vite 8 要求）
- 阿里云百炼 API Key（[获取地址](https://bailian.console.aliyun.com/)，进入控制台 -> API-KEY 管理 -> 创建 API Key）

> **关于登录**：后端默认**拒绝在未配置鉴权时启动**。本地体验可设置
> `AUTH_DISABLED=true`（后端）和 `VITE_AUTH_DISABLED=true`（前端）跳过登录；`./start.sh`
> 在完全没有 Supabase 配置时会自动这样做。要启用真实登录，见下文「登录鉴权」。

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
# 本地不接 Supabase 时，需显式关闭鉴权：
AUTH_DISABLED=true VITE_AUTH_DISABLED=true make dev   # 同时启动前后端开发服务器
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

# 2. 启动后端（本地不接 Supabase 时加 AUTH_DISABLED=true）
go mod tidy
AUTH_DISABLED=true go run cmd/server/main.go

# 3. 新开终端，启动前端
cd web && npm install && VITE_AUTH_DISABLED=true npm run dev
```

前端默认运行在 http://localhost:5173，后端 API 在 http://localhost:8080。

## 项目结构

```
├── cmd/server/main.go          # 程序入口
├── internal/
│   ├── config/                 # 配置管理
│   ├── model/                  # 千问 ChatModel 接入
│   ├── auth/                   # Supabase JWT 校验与鉴权中间件
│   ├── agent/                  # 各功能 Agent 实现
│   ├── prompt/                 # Prompt 模板
│   ├── tool/                   # 文档解析等工具
│   ├── handler/                # HTTP API 处理
│   └── store/                  # 会话和文件存储
├── web/                        # React 前端
├── docs/PRD.md                 # 产品需求文档
└── uploads/                    # 上传文件存储
```

## 登录鉴权（Supabase）

1. 在 [Supabase](https://supabase.com/) 创建项目，**Authentication → Providers** 里启用 Email（是否要求邮箱验证由你决定）。
2. 后端环境变量：
   - `SUPABASE_URL=https://<project>.supabase.co` —— 后端据此拉取公钥（`/auth/v1/.well-known/jwks.json`）校验新版非对称签名的 JWT，并校验签发者 `iss`。
   - 旧项目仍使用 HS256 共享密钥时，改设（或同时设置）`SUPABASE_JWT_SECRET`（Settings → API → JWT Secret）。
3. 前端环境变量（参考 `web/.env.example`，复制为 `web/.env`）：`VITE_SUPABASE_URL`、`VITE_SUPABASE_ANON_KEY`。
   anon key 本就是公开的；**切勿**把 `service_role` key 或 JWT Secret 放进任何 `VITE_*` 变量或提交进仓库。

行为说明：

- 除 `GET /healthz` 外，所有 `/api/*` 接口都要求 `Authorization: Bearer <access_token>`，否则返回 401。
- 用户 ID 取自 token 的 `sub`；会话与上传文件只对创建者可见，访问他人的资源一律返回 404（与"不存在"不可区分）。
- 后端在既没配置 `SUPABASE_URL`/`SUPABASE_JWT_SECRET`、又没设置 `AUTH_DISABLED=true` 时拒绝启动（fail closed）。
- `AUTH_DISABLED=true` 仅用于本地开发：所有请求都以 `dev-user` 身份运行，启动时会打印警告。**不要在部署环境使用**。

## 会话持久化（Supabase Postgres）

同时设置 `SUPABASE_URL` 和 `SUPABASE_PUBLISHABLE_KEY` 后，会话与消息存入 Supabase 的
`chat_sessions` / `chat_messages` 两张表；未设置则退回内存存储（重启即丢，仅适合本地开发）。

1. 在项目里执行迁移 [`supabase/migrations/20261004000000_chat_persistence.sql`](supabase/migrations/20261004000000_chat_persistence.sql)
   （Supabase 控制台 SQL Editor，或 `supabase db push`）。
2. 设置上述两个环境变量。

设计要点：

- 后端**不使用** service_role 密钥或数据库密码。它把已通过校验的**用户自己的 access token** 转发给 Supabase REST 接口
  （`apikey` 头放公开的 publishable key，`Authorization: Bearer` 放用户 token），因此数据库以 `authenticated` 角色执行语句，
  由 **Row Level Security** 强制"只能访问自己的行"。后端查询另外显式带上 `user_id` 过滤，隔离不只依赖 RLS 一层。
- 消息只能写入属于自己的会话；`anon`（未登录）角色没有任何权限；`user_id` 不可被修改。
- 触发器会在新增消息时更新会话的 `updated_at`，并用首条用户消息的前 20 个字符生成标题。
- 上传的文件目前仍在本地磁盘（`UPLOAD_DIR`），不在数据库里。

## API 接口

> 除 `GET /healthz`（健康检查，免鉴权）外，以下接口均需携带 Bearer token，且只能访问当前用户自己的数据。

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
| SUPABASE_URL | 是* | - | Supabase 项目地址，用于获取 JWKS 校验 JWT |
| SUPABASE_JWT_SECRET | 是* | - | 旧项目的 HS256 JWT 密钥（与 SUPABASE_URL 至少设置一个） |
| SUPABASE_PUBLISHABLE_KEY | 否 | - | Supabase publishable（或旧 anon）key，公开密钥。与 `SUPABASE_URL` 一起设置后，会话改存 Supabase Postgres；否则存内存（重启即丢） |
| AUTH_DISABLED | 否 | false | 仅限本地开发：设为 `true` 关闭鉴权 |

\* 未设置 `AUTH_DISABLED=true` 时，`SUPABASE_URL` 与 `SUPABASE_JWT_SECRET` 至少需要设置一个，否则后端拒绝启动。

前端（构建时变量，写入 `web/.env`）：

| 变量 | 说明 |
|------|------|
| VITE_SUPABASE_URL | Supabase 项目地址 |
| VITE_SUPABASE_ANON_KEY | Supabase anon（publishable）key |
| VITE_AUTH_DISABLED | 仅限本地开发：设为 `true` 跳过登录页（需与后端 `AUTH_DISABLED` 一致） |

## License

MIT

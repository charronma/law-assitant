# AI 法律助手

基于 Go [Eino](https://github.com/cloudwego/eino) 框架和通义千问大模型构建的智能法律服务平台。

## 功能模块

- **法律咨询** - 多轮对话式法律问答，覆盖民事/刑事/行政/劳动法等领域
- **诉状撰写** - 自动生成起诉状/答辩状/上诉状等规范法律文书
- **合同优化** - 上传 Word(.docx)/PDF/TXT/MD 合同文件，审查风险条款并生成修改建议。上传时即解析文本：扫描件/图片 PDF、旧版 .doc、损坏文件会被明确拒绝并提示原因，不会静默当作空文档发给模型
- **证据整理** - 对证据材料进行分类、排序，生成规范证据清单
- **取证指导** - 根据案件类型指导证据收集方向和注意事项
- **沟通话术** - 生成律师与客户/当事人的沟通策略和话术模板
- **登录鉴权** - 基于 [Supabase Auth](https://supabase.com/auth)，会话与上传文件按用户隔离
- **部署** - 见 [docs/DEPLOY.md](docs/DEPLOY.md)（Vercel + Docker 后端 + Supabase）

## 技术栈

| 层级 | 技术 |
|------|------|
| 后端框架 | Go + [Eino](https://github.com/cloudwego/eino) |
| 大模型 | 通义千问 / DeepSeek / GLM / Kimi 等，经阿里云百炼 DashScope 的 OpenAI 兼容接口，前端可自由选择 |
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

## 模型选择与额度提示

前端顶栏可以按 **旗舰 / 标准 / 快速** 分组选择模型，选择结果记在浏览器 `localStorage`。

内置的 12 个模型（顺序即推荐顺序，`QWEN_MODELS` 默认值）：

| id | 名称 | 档位 |
|----|------|------|
| qwen3.8-max-0902（默认） | 通义千问 3.8 Max（0902） | 旗舰 |
| qwen3.8-max | 通义千问 3.8 Max | 旗舰 |
| deepseek-v4-pro-0813 | DeepSeek V4 Pro | 旗舰 |
| qwen3.8-2.4t-a95b | 通义千问 3.8 2.4T MoE | 旗舰 |
| glm-5.3 | GLM-5.3 | 旗舰 |
| kimi-k3 | Kimi K3 | 旗舰 |
| qwen3.8-27b | 通义千问 3.8 27B | 标准 |
| deepseek-v4.1-flash | DeepSeek V4.1 Flash | 快速 |
| qwen3.8-flash | 通义千问 3.8 Flash | 快速 |
| qwen3.7-flash | 通义千问 3.7 Flash | 快速 |
| qwen3.7-flash-2026-07-15 | 通义千问 3.7 Flash（0715） | 快速 |
| deepseek-v4-flash-0731 | DeepSeek V4 Flash（0731） | 快速 |

名称和档位来自代码里的内置映射表；白名单里出现表外的 id 也能用（名称显示为 id 本身，档位为"标准"），所以以后加模型只需改环境变量 `QWEN_MODELS`。所有模型共用同一个 `QWEN_BASE_URL` 和 API Key。

> ⚠️ 升级注意：默认模型从 `qwen-max` 改成了 `qwen3.8-max-0902`。如果你设置了自定义的 `QWEN_MODELS` 却没有包含默认模型，需要同时设置 `QWEN_MODEL`，否则后端启动时会报错退出。

**上游错误分类**：后端把模型服务的失败转成结构化错误 `{"code","model","message"}`，**不会**把上游原始响应或密钥返回给前端（原始错误只写服务端日志）：

| 上游情况 | HTTP | code | 前端表现 |
|----------|------|------|----------|
| 403 且表示免费额度用尽（`AllocationQuota.FreeTierOnly` / `Free quota exhausted` / `quota`） | 402 | `QUOTA_EXHAUSTED` | 醒目横幅 + "切换模型" / "换个模型重试"，该模型在下拉框里标记"额度已用完" |
| 401 / `Incorrect API key` | 502 | `INVALID_API_KEY` | 提示联系管理员，不提供重试 |
| 429 | 429 | `RATE_LIMITED` | 提示稍后再试，提供"重试" |
| 其他 | 500 | `UPSTREAM_ERROR` | 显示提示，提供"重试" |

输出开始之后才出错时，用 SSE 的 `event: error` 携带同样的 JSON。被拒绝的请求（额度、限流等）**不会**写入会话历史，所以"换个模型重试"不会产生重复的用户消息。

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
- 每条消息记录本轮使用的模型 id（`chat_messages.model`，迁移 `20261005000000_chat_message_model.sql`）。
- 触发器会在新增消息时更新会话的 `updated_at`，并用首条用户消息的前 20 个字符生成标题。
- 配置了 Supabase 时，上传文件存入私有 Storage bucket `uploads`（路径 `<user_id>/<file_id>.<ext>`），元数据和提取文本存 `public.uploaded_files`（RLS，同样使用用户自己的 JWT），重启/换实例后仍可引用；未配置时退回本地磁盘 + 内存（重启即失效）。需要先应用 `supabase/migrations/20261006000000_uploaded_files.sql`。删除会话不会删除已上传的文件。

## API 接口

> 除 `GET /healthz`（健康检查，免鉴权）外，以下接口均需携带 Bearer token，且只能访问当前用户自己的数据。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/models | 可选模型列表与默认模型：`{"models":[{"id","label","tier"}],"default":"..."}` |
| POST | /api/chat | 发送消息（SSE 流式响应）。可选字段 `model`，缺省使用默认模型，不在白名单内返回 400 `INVALID_MODEL` |
| POST | /api/redline | 对已上传的 .docx 生成**带修订痕迹和批注的修订版**：`{file_id, instruction, model?}` → `{filename, docx_base64, summary, applied[], skipped[]}`。模型给出结构化修改清单，后端把它写进原文件的 `w:ins/w:del` 和批注，保留原格式与已有修订；每个段落写入后会校验“接受修订=预期文本、拒绝修订=原文”，校验不过的修改会被跳过并列在 `skipped` 里，不会产出损坏的文件。非 .docx 返回 422 `NOT_DOCX` |
| GET | /api/files/{id} | 已上传文件的元数据（文件名、大小、字数） |
| POST | /api/export/docx | 把一条回复（Markdown）导出为 Word：`{title?, content}` → `.docx`（标题/列表/表格/加粗/引用，末尾附免责声明） |
| POST | /api/upload | 上传文件（.docx/.pdf/.txt/.md）。成功返回 `chars`/`preview`/`truncated`；无法提取文字时返回 422 `{code,message}`（`NO_TEXT`/`UNSUPPORTED_FORMAT`/`EXTRACT_FAILED`/`FILE_TOO_LARGE`） |
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
| QWEN_MODEL | 否 | qwen3.8-max-0902 | 默认模型。必须在 `QWEN_MODELS` 白名单内，否则后端拒绝启动 |
| QWEN_MODELS | 否 | 内置 12 个模型（见下） | 允许用户选择的模型白名单，逗号分隔，顺序即前端显示顺序 |
| QWEN_BASE_URL | 否 | https://dashscope.aliyuncs.com/compatible-mode/v1 | API 地址 |
| SERVER_PORT | 否 | 8080 | 服务端口 |
| UPLOAD_DIR | 否 | ./uploads | 文件上传目录 |
| CHAT_RATE_PER_MINUTE | 否 | 20 | 每用户每分钟聊天请求数（令牌桶，0=不限）；超出返回 429 `USER_RATE_LIMITED` |
| UPLOAD_RATE_PER_MINUTE | 否 | 10 | 每用户每分钟上传次数（0=不限） |
| MAX_CONCURRENT_CHATS | 否 | 2 | 每用户同时生成的回答数（0=不限）；超出返回 429 `TOO_MANY_STREAMS` |
| MAX_MESSAGE_CHARS | 否 | 8000 | 单条消息最大字符数；超出返回 413 `MESSAGE_TOO_LONG` |
| MAX_DOCUMENT_CHARS | 否 | 200000 | 每次请求发给模型的上传文档文本总字符数（保留最新的；文档在整个对话中都会随请求发送） |
| MAX_HISTORY_CHARS | 否 | 30000 | 发送给模型的历史对话字符预算（只保留最近的整轮消息，数据库里的记录不受影响） |
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

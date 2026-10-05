# 部署指南

```
浏览器 ──► Vercel（web/ 静态前端）
   │
   ├──► Supabase Auth（登录，签发 JWT）
   │
   └──► 后端（Docker，常驻进程）──► 通义千问 DashScope
              └─ 用 Supabase 的公钥（JWKS）校验 JWT
```

后端必须是**常驻进程**（Render / Fly.io / Railway / Cloud Run 等），不能放 Vercel Serverless：
会话保存在进程内存里，且 `/api/chat` 是可能持续数分钟的 SSE 流式响应。

> ⚠️ 当前限制（公开部署前请知晓）
> - 会话与消息存在 Supabase Postgres 里（见第 1 步），重启 / 重新部署不会丢失。
>   **前提是同时设置了 `SUPABASE_URL` 和 `SUPABASE_PUBLISHABLE_KEY`**；漏设时后端会退回内存存储（只在启动日志里打印一条警告，不会报错），务必检查。
> - 上传文件的**元数据和文本缓存在进程内存里**，文件本身在本地磁盘（`UPLOAD_DIR`）：重启后已上传的文件无法再被引用，
>   并且**只能跑 1 个实例**（多实例间互相看不到）。迁移到 Supabase Storage 之前请保持单实例。
> - **没有限流**：任何注册用户都能消耗你的千问额度。
> - CORS 只允许 `FRONTEND_URL` 这一个来源，Vercel 的预览部署域名会被拦截。

## 1. Supabase

1. 创建项目。
2. **Authentication → Providers**：启用 Email（按需决定是否要求邮箱验证）。
3. **Authentication → URL Configuration**：把 Site URL 设为你的前端地址（邮箱验证链接会跳到这里）。
4. 记下：
   - **Project URL**（`https://<ref>.supabase.co`）→ 后端 `SUPABASE_URL`、前端 `VITE_SUPABASE_URL`
   - **anon / publishable key** → 前端 `VITE_SUPABASE_ANON_KEY`
5. 新项目使用非对称签名密钥，后端只需要 `SUPABASE_URL`。如果你的项目仍是旧版 HS256 JWT，
   另外把 **JWT Secret** 设为后端的 `SUPABASE_JWT_SECRET`（后端会根据 token 的签名算法自动选择验证方式）。

6. **创建数据表**：按文件名顺序执行 `supabase/migrations/` 下的全部迁移（目前是 `20261004000000_chat_persistence.sql` 和 `20261005000000_chat_message_model.sql`）。**升级时务必先应用新迁移，再部署新版后端**：新版后端会写入 `chat_messages.model`，列不存在时消息写入会失败。
   首次建库的做法：执行 `supabase/migrations/20261004000000_chat_persistence.sql`
   （控制台 SQL Editor 粘贴运行，或 `supabase link` 后 `supabase db push`）。它会创建 `chat_sessions` / `chat_messages`，
   开启 RLS 并只授权给已登录用户。之后在控制台 **Advisors → Security** 确认没有告警。

> 🔒 **不要**把 `service_role` key 或 JWT Secret 放进前端（任何 `VITE_*` 变量）或提交进仓库。

## 2. 后端（Docker）

仓库根目录的 `Dockerfile` 会构建一个静态二进制镜像。在任意支持 Docker 的平台创建 Web Service，设置：

| 变量 | 值 |
|------|-----|
| `DASHSCOPE_API_KEY` | 百炼 API Key（作为 secret） |
| `SUPABASE_URL` | 上一步的 Project URL |
| `SUPABASE_PUBLISHABLE_KEY` | Supabase 的 publishable key（`sb_publishable_...`，公开密钥）。设置后会话才会持久化 |
| `SUPABASE_JWT_SECRET` | 仅旧版 HS256 项目需要 |
| `QWEN_MODEL` | 默认模型，缺省 `qwen3.8-max-0902`；必须在 `QWEN_MODELS` 内 |
| `QWEN_MODELS` | 可选，用户可选的模型白名单（逗号分隔）；缺省为内置的 12 个。自定义时务必包含默认模型 |
| `QWEN_BASE_URL` | 可选，默认国内站；国际站密钥需设为 `https://dashscope-intl.aliyuncs.com/compatible-mode/v1` |
| `FRONTEND_URL` | 前端的**完整来源**，如 `https://your-app.vercel.app`（无结尾 `/`） |
| `UPLOAD_DIR` | 默认 `/data/uploads`；挂载持久卷到 `/data` 才能保留上传文件 |
| `CHAT_RATE_PER_MINUTE` / `UPLOAD_RATE_PER_MINUTE` / `MAX_CONCURRENT_CHATS` / `MAX_MESSAGE_CHARS` / `MAX_HISTORY_CHARS` | 可选，按用户限流与上下文预算，默认 20 / 10 / 2 / 8000 / 30000，详见 README；限流状态在进程内，多副本时每个副本各自计数 |

- 监听端口：自动读取平台注入的 `PORT`（也可用 `SERVER_PORT`）；镜像默认 8080。
- 健康检查路径：`GET /healthz`（免鉴权）。
- 实例数：**固定为 1**，并关闭"闲置休眠 / 缩容到 0"（否则内存里的会话会丢）。
- **不要**设置 `AUTH_DISABLED`。后端在没有配置鉴权时会拒绝启动，这是有意的。

## 3. 前端（Vercel）

1. Import 仓库，**Root Directory** 选 `web`，Framework 选 Vite（会自动识别）。
2. 环境变量（**构建时**生效，修改后需重新部署）：

   | 变量 | 值 |
   |------|-----|
   | `VITE_SUPABASE_URL` | Supabase Project URL |
   | `VITE_SUPABASE_ANON_KEY` | Supabase anon key |
   | `VITE_API_BASE_URL` | 后端的完整来源，如 `https://your-backend.example.com`（无结尾 `/`） |

   **不要**设置 `VITE_AUTH_DISABLED`。
3. 浏览器直接请求后端（而不是经 Vercel 代理），所以流式响应不受静态托管代理的影响；
   也因此后端的 `FRONTEND_URL` 必须与前端的实际域名一致。

## 4. 冒烟测试

```bash
curl -i https://<backend>/healthz                 # 200 {"status":"ok"}
curl -i https://<backend>/api/sessions            # 401（未带 token，说明鉴权生效）
curl -s https://<ref>.supabase.co/auth/v1/.well-known/jwks.json   # 应返回 {"keys":[...]}，后端靠它验证 token
```

`jwks.json` 里的 `keys` 为空数组，说明这个项目仍在使用旧版 HS256 密钥：此时后端需要额外设置 `SUPABASE_JWT_SECRET`。

然后在浏览器打开前端：注册 / 登录 → 发一条咨询 → 能看到流式回复 → 刷新页面仍保持登录。
如果浏览器控制台出现 CORS 错误，检查后端 `FRONTEND_URL` 是否与前端域名**完全一致**（协议、域名、无结尾 `/`）。

## 5. 公开上线前建议完成

- [x] 会话持久化（Supabase Postgres）
- [ ] 每用户限流 / 额度控制
- [ ] 上传文件迁移到 Supabase Storage（私有 bucket）
- [ ] 评估合同等敏感文件发送给第三方模型的合规与脱敏要求

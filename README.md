# Yuanbao Bridge

将腾讯元宝（Yuanbao）桌面客户端桥接为本地 HTTP API，兼容简易 JSON 接口与 OpenAI Chat Completions 格式。

通过 Chrome DevTools Protocol (CDP) 在已登录的元宝聊天页面内执行 JavaScript，复用客户端内置的 `ybChatService` 发起对话，无需单独维护 Cookie 或 Token。

## 免责声明

**本项目仅供学习研究用途，请于下载后 24 小时删除。**

**本项目不接受任何资金捐助和交易，此项目是纯粹研究交流学习性质。**

**禁止将本项目用于任何盈利或商业行为**（包括但不限于对外提供服务、收费代调用、集成至商业产品或 SaaS、广告变现等）。

**仅限个人学习研究自用，禁止对外提供服务或商用，避免对官方造成服务压力，否则风险自担。**

**因本项目造成的风险或损失，开发者不承担任何法律责任。**

补充说明：

- 本项目与腾讯、元宝（Yuanbao）官方无任何关联，亦未获官方授权或认可。
- 本项目通过对桌面客户端进行逆向分析实现桥接，会修改/生成 `yuanbao-debug.exe` 并开启远程调试端口，可能违反软件许可协议或服务条款；使用前请自行评估合规风险。
- 使用者须遵守元宝用户协议、所在地法律法规及平台规则，不得用于爬取、批量调用、绕过风控、传播违法内容或其他侵害第三方权益的行为。
- 本项目按「现状」提供，不保证可用性、稳定性或与未来客户端版本的兼容性；因客户端更新、账号封禁、接口变更等导致的不可用，开发者不承担责任。
- 使用者应自行承担账号安全、数据隐私与本地安全风险（含 API 无鉴权、CDP 暴露等），**切勿将服务监听地址绑定到 `0.0.0.0` 或暴露至公网**。

## 功能

- 自动从 `yuanbao.exe` 生成带远程调试端口的 `yuanbao-debug.exe`
- 自动启动元宝并等待聊天页就绪
- 文本对话、深度思考模式
- 图片对话（Base64 / URL / 本地上传 / multipart）
- OpenAI 兼容的 `/v1/chat/completions` 端点
- 健康检查 `/health`

## 已知限制

- **不支持系统提示词（system prompt）**：桥接层仅向元宝发送单条用户 `prompt`，底层 `/api/chat` 接口也无 system 字段。请求中的 `role: "system"` 消息会被忽略；`assistant` 历史消息同样不会传入模型。如需固定人设或指令，只能写在用户消息里，或依赖元宝客户端自身的 Agent 配置。
- **不支持多轮上下文拼接**：除 `conversation_id` 续聊外，不会把 `messages` 数组中的历史对话合并后发送；每次请求实质上是「当前用户输入 + 可选图片」。

## 环境要求

| 项目 | 要求 |
|------|------|
| 操作系统 | Windows 10/11 |
| Go | 1.21+（仅编译时需要） |
| 元宝客户端 | 已安装并完成登录 |
| 默认安装路径 | `C:\Program Files\Tencent\Yuanbao` |

## 快速开始

### 1. 编译

```bat
cd yuanbao-bridge
build.bat
```

或手动：

```bat
go mod tidy
go build -ldflags "-s -w" -trimpath -o yuanbao-bridge.exe .
```

### 2. Agent 与模型（通常无需手动配置）

未设置 `agentId` / `model` 时，桥接会通过 **CDP 从当前元宝聊天页自动读取**，来源包括：

- 页面 URL 参数
- `localStorage` / `sessionStorage`（如 `launch-box-model-id` 等）
- 页面内 `ybChatService.getModelList()` 与 `/api/agent/model/list`

因此多数情况下**直接运行即可**，会使用你在元宝客户端里当前选中的智能体与模型。

可选覆盖方式（优先级高于自动检测）：

```bat
set YUANBAO_AGENT_ID=你的AgentId
set YUANBAO_MODEL=你的模型Id
yuanbao-bridge.exe
```

或 `-agent` / `-model` 参数、每次 API 请求中的 `agentId` / `model` 字段。

自动检测结果可通过 `GET /health` 查看（`auto_agent_id`、`auto_model` 字段）。

若自动检测失败，请确保聊天页已打开并登录，或手动指定上述参数。也可在开发者工具 Network 中从 `/api/chat/` 请求体查看 `agentId`、`chatModelId`。

### 3. 运行

```bat
yuanbao-bridge.exe
```

首次运行会：

1. 在安装目录生成 `yuanbao-debug.exe`（注入 `--remote-debugging-port=9333`）
2. 启动元宝并打开聊天页
3. 在 `http://127.0.0.1:8765` 监听 HTTP API

### 4. 验证

```bat
curl http://127.0.0.1:8765/health
```

## 命令行参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-install-dir` | `C:\Program Files\Tencent\Yuanbao` | 元宝安装目录 |
| `-cdp-port` | `9333` | CDP 调试端口 |
| `-host` | `127.0.0.1` | HTTP 监听地址 |
| `-port` | `8765` | HTTP 监听端口 |
| `-agent` | （空） | 默认 Agent ID，也可用 `YUANBAO_AGENT_ID` |
| `-model` | （空） | 默认模型 ID，也可用 `YUANBAO_MODEL` |
| `-skip-launch` | `false` | 不自动启动元宝（需已手动打开 debug 版） |
| `-patch-only` | `false` | 仅生成 `yuanbao-debug.exe` 后退出 |

环境变量：`YUANBAO_INSTALL_DIR`、`YUANBAO_AGENT_ID`、`YUANBAO_MODEL`。

## API 文档

### `GET /health`

检查桥接服务与 CDP 状态。

**响应示例：**

```json
{
  "bridge": "dtmp-ybChatService",
  "status": "ok",
  "cdp_available": true,
  "chat_page_ready": true,
  "chat_page_url": "https://yuanbao.tencent.com/chat/...",
  "auto_agent_id": "<当前页检测到的 AgentId>",
  "auto_model": "<当前页检测到的模型Id>"
}
```

`status` 为 `degraded` 时表示 CDP 或聊天页未就绪，对话请求可能返回 502。

---

### `POST /api/chat`

简易 JSON 对话接口。

**请求体（JSON）：**

```json
{
  "prompt": "你好",
  "conversation_id": "",
  "thinking": false,
  "model": "<你的模型Id>",
  "agentId": "<你的AgentId>",
  "images_base64": [],
  "images": []
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `prompt` | string | 用户消息；纯图片时可省略（默认「请描述这些图片」） |
| `conversation_id` | string | 会话 ID，留空则新建 |
| `thinking` | bool | 是否启用深度思考（使用模型子模型） |
| `model` | string | 模型 ID；省略则从聊天页 CDP 自动检测 |
| `agentId` | string | Agent ID；省略则从聊天页 CDP 自动检测 |
| `images_base64` | string[] | Base64 或 `data:image/...;base64,...` |
| `images` | string[] | `http(s)://` URL 或本机绝对路径 |

**multipart/form-data** 也支持，字段：`prompt`、`conversation_id`、`thinking`、`model`、`agentId`，图片字段名 `images` 或 `image`。

**响应示例：**

```json
{
  "ok": true,
  "response": "你好！有什么可以帮你的？",
  "thinking": "",
  "conversation_id": "xxxxxxxx"
}
```

---

### `POST /v1/chat/completions`

OpenAI 兼容接口，便于接入现有工具链。

**请求示例：**

```json
{
  "model": "<你的模型Id>",
  "agentId": "<你的AgentId>",
  "thinking": true,
  "conversation_id": "",
  "messages": [
    { "role": "user", "content": "用一句话介绍 Go 语言" }
  ]
}
```

多模态（图片）消息：

```json
{
  "model": "<你的模型Id>",
  "agentId": "<你的AgentId>",
  "messages": [
    {
      "role": "user",
      "content": [
        { "type": "text", "text": "这张图里有什么？" },
        { "type": "image_url", "image_url": { "url": "https://example.com/a.png" } }
      ]
    }
  ]
}
```

`image_url.url` 支持 `http(s)://` 与 `data:` URL。响应中 `message.reasoning_content` 携带思考内容（如有），`conversation_id` 在顶层返回。

> **注意**：本接口虽兼容 OpenAI 的 `messages` 格式，但**仅解析最后一条 `role: "user"` 消息**的文本与图片。`system`、`assistant` 等角色消息会被静默忽略，无法通过 `messages` 设置系统提示词或多轮历史。续聊请使用 `conversation_id` 指向元宝已有会话。

## 工作原理

```
HTTP Client
    │
    ▼
yuanbao-bridge (Go HTTP Server)
    │
    ▼
Chrome DevTools Protocol (WebSocket)
    │
    ▼
元宝聊天页 (Electron / Chromium)
    │
    ▼
webpack 模块 ybChatService (410682.W)
    │
    ▼
元宝后端 API (/api/chat, /api/resource/...)
```

1. **Patch**：在 `yuanbao.exe` 内将 Chromium 启动参数字符串等长替换，写入 `yuanbao-debug.exe`，开启 `--remote-debugging-port=9333`。
2. **CDP**：连接聊天页 target，通过 `Runtime.evaluate` 注入异步 JS。
3. **对话**：JS 调用 `ybChatService.request` 创建会话、发送 SSE 流式请求并解析回复。
4. **图片**：通过客户端 `readFile` 读取本地文件，经 `genUploadInfo` + COS 上传后附带 `multimedia` 字段。

## 常见问题

### 能否设置 system prompt / 系统提示词？

不能。元宝聊天 API 与桥接实现均只接受用户侧 `prompt`，没有 system 字段。`/v1/chat/completions` 里传入的 `{"role":"system","content":"..."}` 会被忽略。变通做法：把指令写在用户消息开头（例如「请始终用英文回答。我的问题是：……」），或使用不同 `agentId` 选择元宝内置智能体。

### Patch 失败：browser-args string not found

元宝版本更新后内嵌的 Chromium 参数字符串可能变化。请对照新版本 `yuanbao.exe` 中的启动参数，更新 `patch.go` 中的 `patchOld` / `patchNew`（须保持等长）。

### 502 / chat page not ready

- 确认已登录元宝
- 手动打开聊天页：`https://yuanbao.tencent.com/chat`
- 检查 CDP：`http://127.0.0.1:9333/json/list` 中是否有含 `/chat` 的 page

### webpack module 410682 not found

前端打包 chunk ID 随版本变化。需在新版 JS 中重新定位 `ybChatService` 的 webpack require ID，并更新 `bridge.go` / `bridge_media.go` 中的模块号。

### 图片上传失败

元宝 `readFile` 仅允许访问系统临时目录等白名单路径。桥接会自动将图片复制到 `%TEMP%`；请勿依赖工作区路径直传。

## 安全提示

- 默认仅监听 `127.0.0.1`，**不要**将服务暴露到公网
- API 无鉴权，任何能访问本机的进程均可代发对话
- `yuanbao-debug.exe` 会开启远程调试，请勿在不可信环境长期运行

## 项目结构

```
yuanbao-bridge/
├── main.go           # 入口与 CLI
├── config.go         # 配置与默认值
├── patch.go          # yuanbao-debug.exe 生成
├── launcher.go       # 进程启动与 CDP 等待
├── cdp.go            # CDP WebSocket 客户端
├── bridge.go         # 文本对话 JS 桥接
├── bridge_media.go   # 图片上传 + 对话 JS
├── context.go        # CDP 自动检测 agentId / model
├── api.go            # HTTP 路由与 OpenAI 兼容
├── api_images.go     # 图片请求解析
├── media_helpers.go  # 图片落盘与临时文件
├── build.bat         # Windows 编译脚本
├── go.mod
└── README.md
```

## 许可证

[MIT](LICENSE)

## 贡献

欢迎提交 Issue 与 Pull Request。适配新版本元宝时，请附上版本号与 patch / webpack 模块变更说明。

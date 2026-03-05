# lobster-go

`lobster-go` 是一个用 Go 实现的轻量级 AI Agent 框架，目标是对齐 nanobot 的核心能力，并提供更易部署、可测试、可扩展的工程形态。

## 功能概览

当前已实现：

- CLI 命令：`onboard` / `agent` / `session list` / `cron` / `heartbeat` / `version`
- Provider 抽象：`OpenAI` + `MockProvider`
- Agent 主循环：
  - 消息消费与回复发布
  - Tool Calling（支持同轮多个 tool call）
  - 错误兜底、空回复兜底、最大迭代保护
- 内置工具：
  - `list_dir` / `read_file` / `write_file`
  - `exec`（带超时、危险命令黑名单、输出截断）
  - `web_fetch` / `web_search(stub)`
  - `send_message`
- Session 持久化：JSONL（含缓存、历史读取、legacy 迁移）
- Memory 系统：
  - `MEMORY.md` + `HISTORY.md`
  - `save_memory` 工具调用归档
  - `window` / `archive_all` 模式
  - `last_consolidated` 管理
  - AgentLoop 按阈值自动归档
- 通道层：
  - `Feishu` 适配器（收发、鉴权、去重、权限过滤）
  - `MockChannel`（本地联调与测试）
- 模板系统：`AGENTS.md`、`TOOLS.md`、`USER.md`、`SOUL.md`、`HEARTBEAT.md`、`memory/*`
- 测试体系：单测 + `test/e2e` 端到端回归

## 快速开始

### 1. 环境要求

- Go `1.22+`

### 2. 安装依赖并测试

```bash
go test ./...
```

如果本地环境对 Go cache 有权限限制，可使用：

```bash
GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test ./...
```

### 3. 初始化

```bash
go run ./cmd/lobster-go onboard
```

初始化后会生成：

- 配置文件：`~/.lobster/config.json`
- 工作区：`~/.lobster/workspace`
- 模板与记忆目录：`memory/`, `history/`, `skills/` 等

### 4. 运行

```bash
go run ./cmd/lobster-go version
go run ./cmd/lobster-go help
go run ./cmd/lobster-go agent
go run ./cmd/lobster-go session list
go run ./cmd/lobster-go cron list
```

## 配置示例

`~/.lobster/config.json`（支持 `camelCase` / `snake_case`）

```json
{
  "providers": {
    "openai": {
      "apiKey": "sk-xxx",
      "baseUrl": "https://api.openai.com/v1/chat/completions",
      "model": "gpt-4.1"
    }
  },
  "agents": {
    "defaults": {
      "model": "gpt-4.1",
      "temperature": 0.1,
      "maxTokens": 4096
    }
  },
  "tools": {
    "restrictToWorkspace": true,
    "execTimeoutSec": 120
  },
  "services": {
    "cronIntervalSec": 60,
    "heartbeatIntervalSec": 30
  },
  "memory": {
    "consolidateEvery": 20,
    "windowSize": 50,
    "mode": "window"
  }
}
```

## 使用方式（典型流程）

1. `onboard` 初始化配置与工作区。
2. 在配置中填写 `openai.apiKey`。
3. 启动 `agent`，通过控制台输入消息。
4. Agent 根据模型输出自动调用工具（文件、命令、网页等）。
5. 对话历史写入 session；到达阈值后触发记忆归档，更新 `MEMORY.md` / `HISTORY.md`。

## 实现原理（简述）

核心是“总线解耦 + 可组合 AgentLoop”：

1. Channel 将外部消息转成 `InboundMessage` 放入 `MessageBus`。
2. AgentLoop 消费消息，构造上下文并调用 Provider。
3. 若模型返回工具调用，则执行 Tool 并把结果回灌上下文继续推理。
4. 生成最终回复后，写入 `OutboundMessage`、持久化 Session。
5. 满足阈值时触发 Memory Consolidation（`save_memory`），维护长期记忆。

## 技术架构

```text
[Channels]
   |  inbound/outbound
   v
[MessageBus] <-------------------------------+
   |                                          |
   v                                          |
[AgentLoop] --calls--> [Provider(OpenAI/Mock)]
   |  \\
   |   \\--exec--> [Tools(files/exec/web/message)]
   |
   +--persist--> [Session(JSONL)]
   +--consolidate--> [Memory(MEMORY.md/HISTORY.md)]

[CLI]
  |- onboard / agent / cron / heartbeat / session
```

## 相比 OpenClaw 的优势

> 这里按 OpenClaw 常见形态（动态语言栈、运行时组装）做工程化对比。

- 单二进制部署：Go 静态编译，部署链路简单，运维成本低。
- 资源与并发表现更稳定：goroutine + channel 在高并发消息场景下更轻量。
- 类型安全与重构友好：接口和数据结构强类型，长期演进更稳。
- 本地可控性强：Session(JSONL) + Memory(Markdown) 默认落地，调试与审计直观。
- 测试闭环完整：单测 + e2e 回归已接入，迭代时更容易防回归。

## 当前状态说明

项目已具备“可运行骨架 + 核心链路”。

仍在持续完善的方向包括：

- 更完整的 cron 表达式与去重策略
- heartbeat 指标扩展
- 更多渠道适配（Telegram/Discord/Slack/WhatsApp 等）


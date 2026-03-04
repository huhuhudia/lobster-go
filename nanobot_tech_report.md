# nanobot 技术实现报告

## 概览
nanobot 是一款约 4k 行 Python 3.11 代码的轻量级多渠道 AI 助手。核心特性包含多模型提供商抽象、消息总线解耦的聊天通道、工具调用与子代理、会话与长短期记忆、计划任务、心跳监控、CLI 以及 WhatsApp Node 桥接。依赖栈以 Typer、Pydantic、litellm、httpx、websockets、loguru 为主，测试使用 pytest。

## 顶层架构与数据流
1. Channel 适配器接收外部消息，封装为 `InboundMessage` 推入 `MessageBus.inbound` 队列。
2. `AgentLoop` 消费 inbound，使用 `SessionManager` 获取会话，`ContextBuilder` 拼装历史、模板与技能上下文，调用 LLM Provider。
3. LLM 可能返回 tool calls，经 `ToolRegistry` 调用文件、Shell、Web、消息、Spawn、Cron、MCP 等工具，结果回写上下文。
4. 生成的回复写入 `OutboundMessage` 由 Channel 发送；会话与记忆落盘，必要时触发 `MemoryStore.consolidate`。
5. `CronService` 定时产生系统任务，`HeartbeatService` 周期性生成健康上报，均通过 Bus 与 Agent 解耦。

## 关键模块
1. CLI (`nanobot/cli/commands.py`): 基于 Typer，提供 `onboard`, `agent`, `cron`, `heartbeat`, `version` 等命令，使用 prompt_toolkit 渲染交互输入，Rich 输出，支持历史与终端状态恢复。入口 `python -m nanobot` 调用 Typer app。
2. 配置 (`nanobot/config`): Pydantic Schema 定义 Providers、Agents 默认参数、工具、安全选项及各 Channel 配置，存储于 `~/.nanobot/config.json`，支持 CamelCase 与 snake_case 混用，含迁移逻辑。
3. Provider 抽象 (`nanobot/providers`): 通过 `LLMProvider` 基类统一 `chat` 接口，内置 OpenAI/Codex、LiteLLM、Custom、Transcription 等实现，支持 prompt caching、工具调用、思考模式与多模型选择。
4. 消息与会话 (`nanobot/bus`, `nanobot/session`): `MessageBus` 封装 inbound/outbound asyncio 队列；`SessionManager` 以 JSONL 存储会话，支持迁移旧路径、缓存、清理；`Session.get_history` 保持用户发言对齐，避免工具孤儿。
5. Agent 核心 (`nanobot/agent`): `AgentLoop` 驱动对话迭代，控制工具注册、MCP 连接、子代理管理、工具结果裁剪、防止无限循环；`ContextBuilder` 组装系统/用户/工具上下文；`SubagentManager` 允许在同一 Provider 上递归派生子任务。
6. 工具系统 (`nanobot/agent/tools`): 内置文件读写编辑、目录列表、Shell Exec、Web Search/Fetch、Message 发送、Spawn 子代理、Cron 调度、MCP 资源访问；`ToolRegistry` 暴露 OpenAI-style function schema 并路由调用。
7. 记忆与模板 (`nanobot/agent/memory`, `nanobot/templates`): 长期记忆 `MEMORY.md` 与 `HISTORY.md` 位于工作区 `memory/`；LLM 工具 `save_memory` 汇总历史并更新文件；模板目录提供 AGENTS/TOOLS/USER/HEARTBEAT/SOUL 等 prompt 片段及 memory 模板。
8. 计划任务 (`nanobot/cron`): `CronService` 解析 `cron.yml`，支持最小间隔校验、任务去重与 run-once 语义，`CronTool` 供 Agent 查询/管理；测试覆盖命令与服务行为。
9. 心跳 (`nanobot/heartbeat`): 周期性构建系统状态消息（任务队列长度、活跃会话等），通过 Bus 发送并可用于监控。
10. Channels (`nanobot/channels`): Telegram、Discord、Slack、Feishu、DingTalk、QQ、Matrix、Email、Mochat、WhatsApp 适配器，统一把收到的文本/媒体封装为 InboundMessage，读取 OutboundMessage 发送；具备去重、权限白名单、线程或群组策略等差异化逻辑。
11. Bridge (`bridge/`): Node/TS WebSocket 服务包装 WhatsApp Web 客户端，绑定 127.0.0.1，支持可选令牌认证，将消息/QR/状态推送给 Python 端。
12. Skills (`nanobot/skills`): 轻量技能包结构，含 skill-creator、tmux、cron、memory、weather、summarize、github、clawhub 等，以 SKILL.md 描述工作流，可包含脚本及模板。
13. 辅助与工具链 (`nanobot/utils/helpers.py`): 工作区路径、文件安全名生成、模板同步、代理字符串解析、提示缓存等辅助函数。

## 测试与质量保障
1. 测试框架 pytest，异步场景使用 pytest-asyncio，配置在 pyproject.toml。
2. 覆盖范围包括 CLI 输入处理、命令参数、Cron 服务/命令、Heartbeat 服务、工具验证、消息工具、Feishu 邮件 Matrix 等渠道格式化、上下文与记忆合并、任务取消与并发保护。
3. `core_agent_lines.sh` 用于行数校验，`test_docker.sh` 校验镜像构建。
4. Ruff 作为 linter，line-length 100。

## 部署与运行
1. 安装方式支持源码可编辑安装、uv 工具、PyPI 包，Python 版本要求 ≥3.11。
2. 默认工作区位于 `~/.nanobot/workspace`，首次 `nanobot onboard` 同步模板，生成配置与历史目录。
3. Dockerfile 提供基础镜像，`docker-compose.yml` 组合 agent 与 bridge；bridge 也可独立 `npm install && npm run build` 后运行。

## 已知注意点
1. 多渠道并发依赖 asyncio 与各自 SDK，网络/代理配置需在渠道配置中单独设置。
2. Memory consolidate 依赖 LLM 工具调用，需确保模型支持 function calling；长对话时 `_TOOL_RESULT_MAX_CHARS` 截断工具结果。
3. WhatsApp 依赖 Node 版本与 `@whiskeysockets/baileys`，需要本地持久化 auth 目录与可选 token。
4. 默认不做沙箱隔离，`ExecTool` 可限制 workspace，但配置需显式开启。

# lobster-go 技术方案与里程碑

## 目标与约束
1. 使用 Go 1.22 重写 nanobot，保持目录与功能对齐，仓库名 `lobster-go`，优先完成框架骨架后逐步填充细节。
2. 兼容多渠道聊天、工具调用、子代理、记忆、计划任务、心跳、CLI、WhatsApp 桥接等核心能力；接口语义与用户体验与 nanobot 一致。
3. 保持可测试性，每个里程碑必须有可运行的单元或集成测试并在提交前自测通过。

## 建议目录结构（初版）
```
cmd/lobster-go/main.go          # CLI 入口（cobra）
internal/cli                    # 命令实现
internal/config                 # 配置模型与加载（viper + mapstructure）
internal/bus                    # inbound/outbound 队列接口与实现
internal/session                # 会话与持久化（jsonl）
internal/agent                  # 主循环、上下文构建、子代理、工具注册
internal/agent/tools            # 文件、shell、web、message、spawn、cron、mcp
internal/agent/memory           # MEMORY/HISTORY 管理与合并
internal/providers              # LLM Provider 抽象与实现（OpenAI, LiteLLM 兼容层）
internal/cron                   # CronService 等
internal/heartbeat              # HeartbeatService
internal/channels               # Telegram/Slack/Discord/Feishu/DingTalk/QQ/Matrix/Email/WhatsApp 适配器
internal/bridge                 # WhatsApp WebSocket 服务（可复用 Node 版或 Go 版）
internal/skills                 # 技能模板与脚本（可读写）
internal/templates              # AGENTS/TOOLS/USER/HEARTBEAT/SOUL/MEMORY
pkg/logging                     # log 包装（zap/logrus 选一）
pkg/utils                       # 通用工具（路径、代理、prompt 缓存等）
test/                           # 集成与端到端用例
```

## 里程碑与可验证交付
1. 代码骨架与构建链路：完成 Go modules、Cobra CLI 基础命令 `version`，建立上述目录与占位文件；Tests: `go test ./...` 通过空实现占位测试，确保构建与 lint（golangci-lint）可运行。
2. 配置系统：实现 Config 模型、默认值、加载与保存到 `~/.lobster/config.json`，支持 camelCase/snake_case；Tests: 配置序列化/反序列化、迁移用例。
3. 消息总线与事件模型：定义 Inbound/Outbound 事件结构与 in-memory 队列接口；Tests: 并发生产消费、队列长度统计。
4. Session 与持久化：实现 JSONL 会话存储、历史切片、迁移逻辑；Tests: 保存/加载、截断策略、线程安全。
5. Provider 抽象：定义 Chat API 接口，提供 OpenAI 实现与 Mock Provider，支持工具调用结构；Tests: mock 交互、超时与错误传播。
6. AgentLoop 核心：完成迭代循环、上下文构建、工具注册、工具结果截断、最大迭代保护、子代理管理；Tests: 使用 Mock Provider + Fake Tools 驱动多轮对话，验证工具调用与回复路由。
7. 工具层：实现文件读写编辑、目录列举、Shell Exec（可选工作区限制）、Web Search/Fetch（Brave API stub）、MessageTool、SpawnTool、CronTool、MCP 客户端接口；Tests: 每个工具的功能与错误路径，Exec 沙箱限制，Web 调用超时。
8. 记忆系统：实现 MEMORY/HISTORY 读写与 consolidate 流程（使用 Mock Provider 的 save_memory 工具调用），支持 archive_all 与窗口模式；Tests: 长对话合并、last_consolidated 更新、文件内容校验。
9. Cron 服务：解析 cron 表达式、去重、run-once 支持，与 Agent 通过 Bus 互动；Tests: 时间驱动的任务触发、重复保护、命令行 `lobster-go cron`。
10. Heartbeat 服务：周期收集队列长度、活跃会话等指标，向 Bus 发布 Outbound；Tests: 指标采样与格式验证。
11. CLI 交互：实现 `onboard`, `agent`, `cron`, `heartbeat` 等命令，支持历史、渲染、信号处理；Tests: cobra command 测试、终端输入模拟。
12. Channels 层逐一落地：优先 Telegram/Discord/Slack/Email，后续 Feishu/DingTalk/QQ/Matrix/WhatsApp；Tests: 针对每个渠道的入站解析、出站格式化、权限过滤。支持以 Fake Bus 和 HTTP/WebSocket stub 验证。
13. WhatsApp 桥接：首期可直接重用现有 Node bridge，通过 Go WebSocket 客户端；后续里程碑提供纯 Go 版桥接（可用 baileys port 或开源实现）；Tests: 认证、消息收发、QR 事件转发。
14. 技能与模板同步：实现模板落地到工作区、技能描述读取接口；Tests: 模板同步幂等、缺失文件补齐。
15. 端到端回归：在 `test/e2e` 中用 Mock Provider 驱动代理执行文件读写+工具调用+记忆合并的完整流程；Tests: `go test ./test/e2e -tags=e2e`。

## 测试策略
1. 单元测试覆盖纯逻辑模块（配置、队列、会话、工具、provider mock）。
2. 集成测试覆盖 AgentLoop 与各渠道适配器，使用 httptest/WebSocket 测试服务器。
3. 端到端测试使用 fake provider 与临时工作区目录，验证完整对话、工具调用、记忆持久化。
4. CI 建议：`go test ./...`, `golangci-lint run`, 可选 `docker build` 校验。

## 技术选型与兼容性
1. CLI: Cobra + promptui 或 bubbletea 风格输入；输出用 lipgloss/charmbracelet 生态或直接 fmt + color。
2. 配置: Viper + mapstructure，默认路径 `~/.lobster/config.json`，支持环境变量覆盖。
3. 并发: 使用 context + goroutine + channel；需要全局退出控制和任务取消。
4. 日志: zap 的 sugared logger，支持 JSON 与控制台模式。
5. HTTP/WebSocket: 使用 net/http 与 gorilla/websocket；邮件可用 go-imap 与 gomail。
6. LLM: OpenAI 官方 SDK 或自实现 HTTP 客户端，保留 tool call JSON schema，与 nanobot Prompt 模板兼容。

## 风险与开放问题
1. Matrix、QQ、Feishu、DingTalk Go SDK 的成熟度与维护性需要确认，可能分阶段实现。
2. WhatsApp 纯 Go 实现依赖第三方库稳定性，短期可继续用现有 Node bridge 以降低风险。
3. LLM 工具调用在 Go SDK 中的接口支持有限，可能需要自定义函数调用编码/解码。
4. 多渠道媒体文件传输与富文本格式化差异需逐一适配，测试样例要覆盖边界。

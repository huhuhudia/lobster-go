# lobster-go 技术方案与里程碑

## 目标与约束
1. 使用 Go 1.22 重写 nanobot，保持目录与功能对齐，仓库名 `lobster-go`，优先完成框架骨架后逐步填充细节。
2. 兼容多渠道聊天、工具调用、子代理、记忆、计划任务、心跳、CLI、WhatsApp 桥接等核心能力；接口语义与用户体验与 nanobot 一致。
3. 保持可测试性，每个里程碑必须有可运行的单元或集成测试并在提交前自测通过。
4. 对齐 nanobot 的关键交互：模板（AGENTS/TOOLS/USER/SOUL/HEARTBEAT）、技能体系、完整 cron 语义、更多渠道适配。

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
7. 工具层：实现文件读写编辑、目录列举、Shell Exec（可选工作区限制）、Web Search/Fetch（Brave API stub → 真实 API）、MessageTool、SpawnTool、CronTool、MCP 客户端接口；Tests: 每个工具的功能与错误路径，Exec 沙箱限制，Web 调用超时。
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

## 本地构建与测试基线
1. Go 版本基线：`go 1.22`（见 `go.mod`）。
2. 全量单测：`go test ./...`。
3. 本地受限环境（如沙箱）可使用：
`GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test ./...`
4. CLI 基础验收：
`go run ./cmd/lobster-go version`
`go run ./cmd/lobster-go help`

## 风险与开放问题
1. Matrix、QQ、Feishu、DingTalk Go SDK 的成熟度与维护性需要确认，可能分阶段实现。
2. WhatsApp 纯 Go 实现依赖第三方库稳定性，短期可继续用现有 Node bridge 以降低风险。
3. LLM 工具调用在 Go SDK 中的接口支持有限，可能需要自定义函数调用编码/解码。
4. 多渠道媒体文件传输与富文本格式化差异需逐一适配，测试样例要覆盖边界。

## 下一步工作计划（执行看板）
1. ✅ 现状审阅与差距确认
验收标准：完成对现有 md 与核心代码的整体阅读，明确当前已实现能力、未完成能力和主要阻塞点。

2. ✅ 修复 `internal/templates` 编译阻塞
验收标准：解决 `//go:embed *.md` 无匹配文件导致的编译失败，`go test ./...` 能跑到该包测试阶段。

3. ✅ 对齐 Go 版本与工程基线
验收标准：确认并统一 `go.mod` 版本（目标 Go 1.22），补充必要的构建与本地测试说明。

4. ✅ 加强 Agent 主循环稳定性
验收标准：补齐工具调用失败、空回复、多轮 tool call、超时取消等关键路径测试，并修复发现的问题。

5. ✅ 完成模板系统落盘链路
验收标准：`onboard` 直接复用 `templates.Sync`，保证模板/目录创建幂等，新增对应 CLI 测试。

6. ✅ 打通配置到运行时注入
验收标准：`agent/cron/heartbeat` 从配置读取关键参数（模型、温度、timeout、restrictToWorkspace），并有覆盖测试。

7. ✅ 增强工具安全策略
验收标准：`exec` 工具增加命令黑名单与输出截断策略；文件工具在限制模式下补全越界测试。

8. ✅ 扩展渠道层最小可用集
验收标准：在已有 Feishu 基础上补一个轻量渠道（如 webhook/stdin mock）用于端到端联调，并附测试。

9. ✅ 建立端到端回归用例
验收标准：新增 e2e 用例覆盖“消息入站 -> tool 调用 -> 回复出站 -> session/memory 落盘”主路径。

10. ✅ 收敛里程碑文档与实际状态
验收标准：逐项更新本文里程碑状态，已完成项标记 `✅`，并附可复现的测试命令。

11. ⬜ 上下文拼装与模板注入
验收标准：`Builder` 引入 AGENTS/TOOLS/USER/SOUL/HEARTBEAT 与 MEMORY/HISTORY 的组装逻辑；模板缺失自动同步；新增覆盖测试。

12. ⬜ Web Search 真实接入
验收标准：`web_search` 接入真实 API（如 Brave），支持 `proxy` 与 key；测试覆盖空 query、超时、失败路径。

13. ⬜ Cron 语义补齐
验收标准：支持 cron 表达式、去重、run-once；`cron` CLI/list 输出真实任务；新增行为测试。

14. ⬜ Heartbeat 指标扩展
验收标准：输出队列长度、活跃会话、最近错误等指标；支持读取 HEARTBEAT.md 任务；测试覆盖。

15. ⬜ 关键工具补齐
验收标准：补 `edit_file`、`spawn`、`cron tool`、`mcp client`；新增单测覆盖。

16. ⬜ 渠道矩阵扩展
验收标准：落地 Telegram/Slack/Discord/Email 至少一项；各自入站/出站与权限测试。

17. ⬜ Skills 加载与执行
验收标准：读取 `skills/*/SKILL.md` 并支持最小工作流（指令发现/执行/模板注入）；新增测试。

## 当前里程碑状态（2026-03-06）
1. ✅ 里程碑 1（代码骨架与构建链路）
2. ✅ 里程碑 2（配置系统）
3. ✅ 里程碑 3（消息总线与事件模型）
4. ✅ 里程碑 4（Session 与持久化）
5. ✅ 里程碑 5（Provider 抽象）
6. ✅ 里程碑 6（AgentLoop 核心，含多 tool call/错误兜底/迭代保护）
7. ✅ 里程碑 7（工具层基础完成：文件/exec/web_fetch/web_search stub/message；缺 edit_file、spawn、cron tool、MCP）
8. ✅ 里程碑 8（记忆系统）已支持 Provider `save_memory` 工具调用、`archive_all/window` 两种模式、`last_consolidated` 更新、文件校验测试，并接入 AgentLoop 按阈值自动归档。
9. ⚠️ 里程碑 9（Cron 服务）当前为固定间隔任务模型，尚未实现完整 cron 表达式与去重语义。
10. ⚠️ 里程碑 10（Heartbeat）已支持定时发布，指标维度与 HEARTBEAT.md 任务语义未对齐。
11. ✅ 里程碑 11（CLI 交互基础命令）
12. ⚠️ 里程碑 12（Channels）Feishu + Mock 已落地，其它渠道待补充；Feishu 表格解析 TODO。
13. ⬜ 里程碑 13（WhatsApp 桥接）未开始。
14. ✅ 里程碑 14（模板同步）
15. ✅ 里程碑 15（端到端回归基础用例）

## 已完成工作总结（2026-03-06）
1. Provider 层兼容性修复
修复了 `tool_calls` schema 与 DashScope/OpenAI 兼容性问题，新增 `ToolCallAdapter` 适配层，统一输出 `function` 结构，避免 `invalid_parameter_error`。

2. AgentLoop 工具调用协议修正
严格补齐 `assistant(tool_calls)` 与 `tool(tool_call_id)` 顺序，保证符合 OpenAI/DashScope 规则，新增回归测试防回退。

3. 记忆系统完整落地
支持 `save_memory` 工具调用、`window/archive_all` 模式、`last_consolidated` 更新，并接入 AgentLoop 自动归档。

4. TUI 交互升级（Bubble Tea）
新增 TUI 模式，自动回退到行式模式；支持滚动/自动跟随、状态栏、loading 动画、输入聚焦样式。
依赖拉取需在可用 GOPROXY 下完成。

5. CLI 诊断与日志
启动时输出配置摘要（provider/model/base_url/api_key），接口错误包含请求 URL 与错误体，方便排查鉴权和参数问题。

## 可复现验收命令
1. 全量单测：
`go test ./...`
2. e2e 回归：
`go test ./test/e2e -tags=e2e`
3. CLI 基础验收：
`go run ./cmd/lobster-go help`
`go run ./cmd/lobster-go version`
4. 受限环境可用：
`GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test ./...`
`GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test ./test/e2e -tags=e2e`

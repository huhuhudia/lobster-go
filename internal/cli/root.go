package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/huhuhudia/lobster-go/internal/agent"
	agentctx "github.com/huhuhudia/lobster-go/internal/agent/context"
	"github.com/huhuhudia/lobster-go/internal/agent/tools"
	"github.com/huhuhudia/lobster-go/internal/bus"
	"github.com/huhuhudia/lobster-go/internal/config"
	"github.com/huhuhudia/lobster-go/internal/cron"
	"github.com/huhuhudia/lobster-go/internal/heartbeat"
	"github.com/huhuhudia/lobster-go/internal/providers"
	"github.com/huhuhudia/lobster-go/internal/session"
	"github.com/huhuhudia/lobster-go/internal/templates"
	"github.com/huhuhudia/lobster-go/internal/version"
)

// Execute is the entrypoint used by main. It returns an exit code for os.Exit.
func Execute() int {
	return Run(os.Args[1:], os.Stdout, os.Stderr)
}

// Run parses args and executes the command. It is separated for testability.
func Run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		printHelp(stdout)
		return 0
	}

	switch strings.ToLower(args[0]) {
	case "version", "-v", "--version":
		fmt.Fprintln(stdout, version.Version)
		return 0
	case "help", "-h", "--help":
		printHelp(stdout)
		return 0
	case "onboard":
		return runOnboard(stdout, stderr)
	case "agent":
		return runAgent(stdout, stderr)
	case "session":
		return runSession(args[1:], stdout, stderr)
	case "cron":
		return runCron(args[1:], stdout, stderr)
	case "heartbeat":
		return runHeartbeat(stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		printHelp(stderr)
		return 1
	}
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "lobster-go - lightweight agent framework (Go)")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  lobster-go onboard    Initialize workspace and config")
	fmt.Fprintln(w, "  lobster-go agent      Run interactive agent loop")
	fmt.Fprintln(w, "  lobster-go session list   List sessions")
	fmt.Fprintln(w, "  lobster-go cron       Run cron service")
	fmt.Fprintln(w, "  lobster-go heartbeat  Run heartbeat service")
	fmt.Fprintln(w, "  lobster-go version    Show version")
	fmt.Fprintln(w, "  lobster-go help       Show this help")
}

func loadConfigOrDefault(stderr io.Writer) config.Config {
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(stderr, "warning: load config failed, using defaults: %v\n", err)
		return config.DefaultConfig()
	}
	return cfg
}

func agentLoopConfigFromConfig(cfg config.Config, workspace string) agent.LoopConfig {
	timeoutSec := cfg.Tools.ExecTimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = config.DefaultConfig().Tools.ExecTimeoutSec
	}
	return agent.LoopConfig{
		Model:                  cfg.Agents.Defaults.Model,
		Temperature:            cfg.Agents.Defaults.Temperature,
		MaxTokens:              cfg.Agents.Defaults.MaxTokens,
		Workspace:              workspace,
		RestrictToWorkspace:    cfg.Tools.RestrictToWorkspace,
		ExecTimeoutSec:         timeoutSec,
		MemoryConsolidateEvery: cfg.Memory.ConsolidateEvery,
		MemoryWindowSize:       cfg.Memory.WindowSize,
		MemoryMode:             cfg.Memory.Mode,
	}
}

func durationFromSec(sec int, fallback int) time.Duration {
	if sec <= 0 {
		sec = fallback
	}
	return time.Duration(sec) * time.Second
}

func runAgent(stdout, stderr io.Writer) int {
	cfg := loadConfigOrDefault(stderr)

	workspace := "."
	b := bus.New(100)
	sessions := session.NewManager(workspace)
	builder := agentctx.Builder{SystemPrompt: "You are lobster-go agent."}
	prov := providers.BuildProvider(cfg)
	loopCfg := agentLoopConfigFromConfig(cfg, workspace)
	loop := agent.NewLoop(b, prov, sessions, builder, loopCfg)
	loop.RegisterTool(tools.ListDirTool{Workspace: workspace, Restrict: loopCfg.RestrictToWorkspace})
	loop.RegisterTool(tools.ReadFileTool{Workspace: workspace, Restrict: loopCfg.RestrictToWorkspace, MaxBytes: 4000})
	loop.RegisterTool(tools.WriteFileTool{Workspace: workspace, Restrict: loopCfg.RestrictToWorkspace})
	loop.RegisterTool(tools.ExecTool{Workspace: workspace, Restrict: loopCfg.RestrictToWorkspace, TimeoutSec: loopCfg.ExecTimeoutSec})
	loop.RegisterTool(tools.WebSearchTool{})
	loop.RegisterTool(tools.WebFetchTool{TimeoutSec: 10})
	loop.RegisterTool(tools.MessageTool{Bus: b})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.Run(ctx)

	if shouldUseTUI(stdout) {
		return runAgentTUI(ctx, cancel, b, cfg, stdout, stderr)
	}
	logAgentConfig(stdout, cfg)
	return runAgentLineUI(ctx, cancel, b, stdout)
}

func runAgentLineUI(ctx context.Context, cancel context.CancelFunc, b *bus.MessageBus, stdout io.Writer) int {
	ui := newAgentUI(stdout)
	ui.RenderHeader()
	ui.System("agent running. Type and press Enter to chat. Ctrl+C or /exit to quit.")

	// Outbound printer for cli channel
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			msg, err := b.ConsumeOutbound(ctx)
			if err != nil {
				return
			}
			if msg.Channel != "cli" {
				continue
			}
			ui.Assistant(msg.Content)
		}
	}()

	// Inbound from stdin unless running in timed exit mode
	if os.Getenv("LOBSTER_AGENT_EXIT_AFTER_MS") == "" {
		go func() {
			scanner := bufio.NewScanner(os.Stdin)
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				ui.Prompt()
				if !scanner.Scan() {
					ui.InputSubmitted()
					cancel()
					return
				}
				ui.InputSubmitted()
				text := strings.TrimSpace(scanner.Text())
				if text == "" {
					continue
				}
				switch strings.ToLower(text) {
				case "/help":
					ui.System("commands: /help  /clear  /exit")
					continue
				case "/clear":
					ui.Clear()
					ui.RenderHeader()
					continue
				case "/exit", "/quit":
					ui.System("bye")
					cancel()
					return
				}
				ui.System("thinking...")
				_ = b.PublishInbound(bus.InboundMessage{
					Channel: "cli",
					ChatID:  "console",
					Content: text,
				})
			}
		}()
	}

	if durStr := os.Getenv("LOBSTER_AGENT_EXIT_AFTER_MS"); durStr != "" {
		dur, err := time.ParseDuration(durStr)
		if err != nil {
			dur, _ = time.ParseDuration(durStr + "ms")
		}
		if dur <= 0 {
			dur = 200 * time.Millisecond
		}
		select {
		case <-time.After(dur):
			return 0
		case <-ctx.Done():
			return 0
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sigCh:
		return 0
	case <-ctx.Done():
		return 0
	}
}

func shouldUseTUI(stdout io.Writer) bool {
	if os.Getenv("LOBSTER_DISABLE_TUI") == "1" {
		return false
	}
	if os.Getenv("LOBSTER_AGENT_EXIT_AFTER_MS") != "" {
		return false
	}
	if stdout != os.Stdout {
		return false
	}
	term := strings.TrimSpace(os.Getenv("TERM"))
	if term == "" || term == "dumb" {
		return false
	}
	if fi, err := os.Stdin.Stat(); err != nil || (fi.Mode()&os.ModeCharDevice) == 0 {
		return false
	}
	return true
}

type agentUI struct {
	out         io.Writer
	mu          sync.Mutex
	useColor    bool
	promptShown bool
}

func newAgentUI(out io.Writer) *agentUI {
	term := strings.TrimSpace(os.Getenv("TERM"))
	useColor := term != "" && term != "dumb"
	return &agentUI{out: out, useColor: useColor}
}

func (u *agentUI) RenderHeader() {
	u.mu.Lock()
	defer u.mu.Unlock()
	fmt.Fprintln(u.out, "+---------------------------------------------------+")
	fmt.Fprintln(u.out, "| lobster-go interactive shell                      |")
	fmt.Fprintln(u.out, "| commands: /help /clear /exit                      |")
	fmt.Fprintln(u.out, "+---------------------------------------------------+")
}

func (u *agentUI) Prompt() {
	u.mu.Lock()
	defer u.mu.Unlock()
	fmt.Fprint(u.out, u.promptPrefix())
	u.promptShown = true
}

func (u *agentUI) InputSubmitted() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.promptShown = false
}

func (u *agentUI) Assistant(text string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.beginMessage()
	fmt.Fprintf(u.out, "%s%s\n", u.color("ai  > ", "36"), text)
	u.restorePromptIfNeeded()
}

func (u *agentUI) System(text string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.beginMessage()
	fmt.Fprintf(u.out, "%s%s\n", u.color("sys > ", "33"), text)
	u.restorePromptIfNeeded()
}

func (u *agentUI) Clear() {
	u.mu.Lock()
	defer u.mu.Unlock()
	fmt.Fprint(u.out, "\033[2J\033[H")
}

func (u *agentUI) color(s, code string) string {
	if !u.useColor {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

func (u *agentUI) promptPrefix() string {
	return u.color("you > ", "32")
}

func (u *agentUI) beginMessage() {
	if u.promptShown {
		fmt.Fprint(u.out, "\n")
	}
}

func (u *agentUI) restorePromptIfNeeded() {
	if u.promptShown {
		fmt.Fprint(u.out, u.promptPrefix())
	}
}

func logAgentConfig(stdout io.Writer, cfg config.Config) {
	providerName := strings.TrimSpace(cfg.Agents.Defaults.Provider)
	if providerName == "" {
		if _, ok := cfg.Providers["openai"]; ok {
			providerName = "openai"
		} else {
			providerName = "(auto)"
		}
	}

	model := strings.TrimSpace(cfg.Agents.Defaults.Model)
	if model == "" && providerName != "(auto)" {
		if p, ok := cfg.Providers[providerName]; ok {
			model = p.Model
		}
	}
	if model == "" {
		model = "(default)"
	}

	baseURL := ""
	apiKey := ""
	if p, ok := cfg.Providers[providerName]; ok {
		baseURL = p.BaseURL
		apiKey = p.APIKey
	}
	if baseURL == "" {
		baseURL = "(default)"
	}
	if apiKey == "" {
		apiKey = "(empty)"
	} else {
		apiKey = formatAPIKeyForLog(apiKey)
	}

	fmt.Fprintf(stdout, "config: provider=%s model=%s\n", providerName, model)
	fmt.Fprintf(stdout, "config: base_url=%s\n", baseURL)
	fmt.Fprintf(stdout, "config: api_key=%s\n", apiKey)
}

func formatAPIKeyForLog(key string) string {
	if os.Getenv("LOBSTER_LOG_FULL_API_KEY") == "1" {
		return key
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "(empty)"
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

func runSession(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "session subcommand required (e.g. list)")
		return 1
	}
	switch strings.ToLower(args[0]) {
	case "list":
		return sessionList(stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown session command: %s\n", args[0])
		return 1
	}
}

func sessionList(stdout, stderr io.Writer) int {
	workspace := "."
	manager := session.NewManager(workspace)
	sessions, err := manager.List()
	if err != nil {
		fmt.Fprintf(stderr, "list sessions error: %v\n", err)
		return 1
	}
	if len(sessions) == 0 {
		fmt.Fprintln(stdout, "(no sessions)")
		return 0
	}
	for _, s := range sessions {
		fmt.Fprintf(stdout, "%s\t%d messages\n", s.Key, len(s.Messages))
	}
	return 0
}

// ============================================================================
// Onboard Command
// ============================================================================

func runOnboard(stdout, stderr io.Writer) int {
	// Get config path
	cfgPath, err := config.Path()
	if err != nil {
		fmt.Fprintf(stderr, "error getting config path: %v\n", err)
		return 1
	}

	// Check if config exists
	_, err = os.Stat(cfgPath)
	configExists := err == nil

	if configExists {
		fmt.Fprintf(stdout, "Config already exists at %s\n", cfgPath)
		fmt.Fprintln(stdout, "  y = overwrite with defaults")
		fmt.Fprintln(stdout, "  N = refresh config, keeping existing values")
		fmt.Fprint(stdout, "Overwrite? [y/N]: ")

		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))

		if input == "y" {
			cfg := config.DefaultConfig()
			if err := config.Save(cfg, ""); err != nil {
				fmt.Fprintf(stderr, "error saving config: %v\n", err)
				return 1
			}
			fmt.Fprintf(stdout, "✓ Config reset to defaults at %s\n", cfgPath)
		} else {
			cfg, err := config.Load("")
			if err != nil {
				fmt.Fprintf(stderr, "error loading config: %v\n", err)
				return 1
			}
			if err := config.Save(cfg, ""); err != nil {
				fmt.Fprintf(stderr, "error saving config: %v\n", err)
				return 1
			}
			fmt.Fprintf(stdout, "✓ Config refreshed at %s (existing values preserved)\n", cfgPath)
		}
	} else {
		cfg := config.DefaultConfig()
		if err := config.Save(cfg, ""); err != nil {
			fmt.Fprintf(stderr, "error saving config: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "✓ Created config at %s\n", cfgPath)
	}

	// Create workspace
	home, _ := os.UserHomeDir()
	workspace := filepath.Join(home, ".lobster", "workspace")
	if _, err := os.Stat(workspace); os.IsNotExist(err) {
		if err := os.MkdirAll(workspace, 0o755); err != nil {
			fmt.Fprintf(stderr, "error creating workspace: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "✓ Created workspace at %s\n", workspace)
	}

	added, err := templates.Sync(workspace)
	if err != nil {
		fmt.Fprintf(stderr, "error syncing templates: %v\n", err)
		return 1
	}
	for _, name := range added {
		fmt.Fprintf(stdout, "✓ Created %s\n", name)
	}

	fmt.Fprintln(stdout, "\n🦞 lobster-go is ready!")
	fmt.Fprintln(stdout, "\nNext steps:")
	fmt.Fprintf(stdout, "  1. Add your API key to %s\n", cfgPath)
	fmt.Fprintln(stdout, "     Get one at: https://openrouter.ai/keys")
	fmt.Fprintln(stdout, "  2. Chat: lobster-go agent")

	return 0
}

// ============================================================================
// Cron Command
// ============================================================================

func runCron(args []string, stdout, stderr io.Writer) int {
	cfg := loadConfigOrDefault(stderr)
	interval := durationFromSec(cfg.Services.CronIntervalSec, 60)

	if len(args) > 0 && strings.ToLower(args[0]) == "list" {
		return cronList(stdout, stderr, interval)
	}

	// Run cron service
	fmt.Fprintln(stdout, "Starting cron service...")

	// Create sample jobs
	jobs := []cron.Job{
		{
			Name:     "heartbeat",
			Interval: interval,
			Task:     cron.LogTask("heartbeat"),
		},
	}

	svc := cron.New(jobs)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go svc.Start(ctx)

	fmt.Fprintln(stdout, "Cron service running. Press Ctrl+C to stop.")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	svc.Stop()
	fmt.Fprintln(stdout, "Cron service stopped.")
	return 0
}

func cronList(stdout, stderr io.Writer, interval time.Duration) int {
	fmt.Fprintln(stdout, "Scheduled jobs:")
	fmt.Fprintf(stdout, "  heartbeat  - every %ds\n", int(interval.Seconds()))
	return 0
}

// ============================================================================
// Heartbeat Command
// ============================================================================

func runHeartbeat(stdout, stderr io.Writer) int {
	fmt.Fprintln(stdout, "Starting heartbeat service...")
	cfg := loadConfigOrDefault(stderr)

	b := bus.New(100)
	svc := &heartbeat.Service{
		Bus:      b,
		Interval: durationFromSec(cfg.Services.HeartbeatIntervalSec, 30),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Outbound printer
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			msg, err := b.ConsumeOutbound(ctx)
			if err != nil {
				return
			}
			fmt.Fprintf(stdout, "[%s] %s\n", time.Now().Format("15:04:05"), msg.Content)
		}
	}()

	go svc.Start(ctx)

	fmt.Fprintln(stdout, "Heartbeat service running. Press Ctrl+C to stop.")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	fmt.Fprintln(stdout, "Heartbeat service stopped.")
	return 0
}

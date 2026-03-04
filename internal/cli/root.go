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

func runAgent(stdout, stderr io.Writer) int {
	cfg, _ := config.Load("")

	workspace := "."
	b := bus.New(100)
	sessions := session.NewManager(workspace)
	builder := agentctx.Builder{SystemPrompt: "You are lobster-go agent."}
	prov := providers.BuildProvider(cfg)
	loop := agent.NewLoop(b, prov, sessions, builder, agent.LoopConfig{ExecTimeoutSec: 30, Workspace: workspace})
	loop.RegisterTool(tools.ListDirTool{Workspace: workspace})
	loop.RegisterTool(tools.ReadFileTool{Workspace: workspace, MaxBytes: 4000})
	loop.RegisterTool(tools.WriteFileTool{Workspace: workspace})
	loop.RegisterTool(tools.ExecTool{Workspace: workspace, TimeoutSec: 30})
	loop.RegisterTool(tools.WebSearchTool{})
	loop.RegisterTool(tools.WebFetchTool{TimeoutSec: 10})
	loop.RegisterTool(tools.MessageTool{Bus: b})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.Run(ctx)

	fmt.Fprintln(stdout, "agent running (stub). Type and press Enter to chat. Press Ctrl+C to exit.")

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
			fmt.Fprintf(stdout, "agent: %s\n", msg.Content)
		}
	}()

	// Inbound from stdin unless running in timed exit mode
	if os.Getenv("LOBSTER_AGENT_EXIT_AFTER_MS") == "" {
		go func() {
			scanner := bufio.NewScanner(os.Stdin)
			for scanner.Scan() {
				select {
				case <-ctx.Done():
					return
				default:
				}
				text := strings.TrimSpace(scanner.Text())
				if text == "" {
					continue
				}
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

	// Create memory directory
	memoryDir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "error creating memory directory: %v\n", err)
		return 1
	}

	// Create default MEMORY.md if not exists
	memoryFile := filepath.Join(memoryDir, "MEMORY.md")
	if _, err := os.Stat(memoryFile); os.IsNotExist(err) {
		defaultMemory := "# Auto Memory\n\nThis file stores persistent memories.\n"
		if err := os.WriteFile(memoryFile, []byte(defaultMemory), 0o644); err != nil {
			fmt.Fprintf(stderr, "error creating MEMORY.md: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "✓ Created memory/MEMORY.md\n")
	}

	// Create history directory
	historyDir := filepath.Join(workspace, "history")
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "error creating history directory: %v\n", err)
		return 1
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
	if len(args) > 0 && strings.ToLower(args[0]) == "list" {
		return cronList(stdout, stderr)
	}

	// Run cron service
	fmt.Fprintln(stdout, "Starting cron service...")

	// Create sample jobs
	jobs := []cron.Job{
		{
			Name:     "heartbeat",
			Interval: 60 * time.Second,
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

func cronList(stdout, stderr io.Writer) int {
	fmt.Fprintln(stdout, "Scheduled jobs:")
	fmt.Fprintln(stdout, "  heartbeat  - every 60s")
	return 0
}

// ============================================================================
// Heartbeat Command
// ============================================================================

func runHeartbeat(stdout, stderr io.Writer) int {
	fmt.Fprintln(stdout, "Starting heartbeat service...")

	b := bus.New(100)
	svc := &heartbeat.Service{
		Bus:      b,
		Interval: 30 * time.Second,
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

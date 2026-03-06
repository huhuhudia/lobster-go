package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/huhuhudia/lobster-go/internal/agent"
	agentctx "github.com/huhuhudia/lobster-go/internal/agent/context"
	"github.com/huhuhudia/lobster-go/internal/agent/tools"
	"github.com/huhuhudia/lobster-go/internal/bus"
	"github.com/huhuhudia/lobster-go/internal/channels"
	"github.com/huhuhudia/lobster-go/internal/config"
	"github.com/huhuhudia/lobster-go/internal/cron"
	"github.com/huhuhudia/lobster-go/internal/heartbeat"
	"github.com/huhuhudia/lobster-go/internal/providers"
	"github.com/huhuhudia/lobster-go/internal/session"
	"github.com/huhuhudia/lobster-go/pkg/logging"
)

// Service wires agent loop, channels, and optional services.
type Service struct {
	Bus        *bus.MessageBus
	Agent      *agent.AgentLoop
	Channels   map[string]channels.Channel
	Cron       *cron.Service
	Heartbeat  *heartbeat.Service
	HTTPServer *http.Server
}

// Build constructs a gateway service from config and workspace.
func Build(cfg config.Config, workspace string) (*Service, error) {
	if workspace == "" {
		workspace = "."
	}
	b := bus.New(200)
	b.SetInboundLogger(func(msg bus.InboundMessage) {
		content := truncate(msg.Content, 200)
		logging.Default.Info("inbound: channel=%s sender=%s chat=%s content=%s", msg.Channel, msg.SenderID, msg.ChatID, content)
	})
	sessions := session.NewManager(filepath.Join(workspace, "sessions"))
	builder := agentctx.Builder{SystemPrompt: "You are lobster-go agent.", Workspace: workspace}
	prov := providers.BuildProvider(cfg)

	timeoutSec := cfg.Tools.ExecTimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = config.DefaultConfig().Tools.ExecTimeoutSec
	}
	llmTimeoutSec := cfg.Agents.Defaults.LLMTimeoutSec
	if llmTimeoutSec <= 0 {
		llmTimeoutSec = timeoutSec
	}
	loopCfg := agent.LoopConfig{
		Model:                  cfg.Agents.Defaults.Model,
		Temperature:            cfg.Agents.Defaults.Temperature,
		MaxTokens:              cfg.Agents.Defaults.MaxTokens,
		Workspace:              workspace,
		RestrictToWorkspace:    cfg.Tools.RestrictToWorkspace,
		ExecTimeoutSec:         timeoutSec,
		LLMTimeoutSec:          llmTimeoutSec,
		MemoryConsolidateEvery: cfg.Memory.ConsolidateEvery,
		MemoryWindowSize:       cfg.Memory.WindowSize,
		MemoryMode:             cfg.Memory.Mode,
	}
	loop := agent.NewLoop(b, prov, sessions, builder, loopCfg)
	loop.RegisterTool(tools.ListDirTool{Workspace: workspace, Restrict: loopCfg.RestrictToWorkspace})
	loop.RegisterTool(tools.ReadFileTool{Workspace: workspace, Restrict: loopCfg.RestrictToWorkspace, MaxBytes: 4000})
	loop.RegisterTool(tools.WriteFileTool{Workspace: workspace, Restrict: loopCfg.RestrictToWorkspace})
	loop.RegisterTool(tools.ExecTool{Workspace: workspace, Restrict: loopCfg.RestrictToWorkspace, TimeoutSec: loopCfg.ExecTimeoutSec})
	loop.RegisterTool(tools.WebSearchTool{})
	loop.RegisterTool(tools.WebFetchTool{TimeoutSec: 10})
	loop.RegisterTool(tools.MessageTool{Bus: b})

	chs := map[string]channels.Channel{}
	if cfg.Channels.Feishu.Enabled {
		chs["feishu"] = channels.NewFeishuChannel(cfg.Channels.Feishu, b)
	}
	if cfg.Channels.Mock.Enabled {
		chs["mock"] = channels.NewMockChannel(cfg.Channels.Mock, b)
	}

	hb := &heartbeat.Service{
		Bus:      b,
		Interval: durationFromSec(cfg.Services.HeartbeatIntervalSec, 30),
	}

	return &Service{
		Bus:       b,
		Agent:     loop,
		Channels:  chs,
		Heartbeat: hb,
	}, nil
}

// Run starts the gateway and blocks until ctx is done.
func (s *Service) Run(ctx context.Context) error {
	if s.Bus == nil {
		return fmt.Errorf("bus is required")
	}
	if s.Agent != nil {
		go func() {
			if err := s.Agent.Run(ctx); err != nil {
				logging.Default.Error("agent loop stopped: %v", err)
			}
		}()
	}
	if s.Heartbeat != nil {
		go s.Heartbeat.Start(ctx)
	}
	if s.Cron != nil {
		go s.Cron.Start(ctx)
	}
	if len(s.Channels) > 0 {
		for name, ch := range s.Channels {
			channelName := name
			channel := ch
			go func() {
				if err := channel.Start(ctx); err != nil {
					logging.Default.Error("channel %s start error: %v", channelName, err)
				}
			}()
		}
		go s.dispatchOutbound(ctx)
	}

	<-ctx.Done()
	for name, ch := range s.Channels {
		if err := ch.Stop(); err != nil {
			logging.Default.Warn("channel %s stop error: %v", name, err)
		}
	}
	if s.Cron != nil {
		s.Cron.Stop()
	}
	return nil
}

func (s *Service) dispatchOutbound(ctx context.Context) {
	for {
		msg, err := s.Bus.ConsumeOutbound(ctx)
		if err != nil {
			return
		}
		ch, ok := s.Channels[msg.Channel]
		if !ok {
			if msg.Channel != "heartbeat" {
				logging.Default.Warn("no channel for outbound: %s", msg.Channel)
			}
			continue
		}
		if err := ch.Send(ctx, msg); err != nil {
			logging.Default.Error("send outbound failed: %v", err)
		}
	}
}

// StartHTTP starts the gateway HTTP server for webhook-based channels.
func (s *Service) StartHTTP(ctx context.Context, addr string) error {
	if strings.TrimSpace(addr) == "" {
		addr = ":18790"
	}
	feishu, ok := s.Channels["feishu"].(*channels.FeishuChannel)
	if !ok || feishu == nil {
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/feishu/webhook", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read body failed", http.StatusBadRequest)
			return
		}
		var pre struct {
			Type      string `json:"type"`
			Challenge string `json:"challenge"`
			Token     string `json:"token"`
			Encrypt   string `json:"encrypt"`
		}
		_ = json.Unmarshal(body, &pre)
		if pre.Encrypt != "" {
			http.Error(w, "encrypted payload not supported", http.StatusBadRequest)
			return
		}
		if pre.Type == "url_verification" {
			if feishu.Config.VerificationToken != "" && pre.Token != feishu.Config.VerificationToken {
				http.Error(w, "invalid verification token", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"challenge": pre.Challenge})
			return
		}
		if err := feishu.HandleWebhookEvent(r.Context(), body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{Addr: addr, Handler: mux}
	s.HTTPServer = srv
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logging.Default.Error("gateway http server error: %v", err)
		}
	}()
	logging.Default.Info("gateway http server listening on %s", addr)
	return nil
}

func durationFromSec(sec int, fallback int) time.Duration {
	if sec <= 0 {
		sec = fallback
	}
	return time.Duration(sec) * time.Second
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}

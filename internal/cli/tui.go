package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/huhuhudia/lobster-go/internal/bus"
	"github.com/huhuhudia/lobster-go/internal/config"
)

type tuiMessage struct {
	role    string
	content string
}

var (
	colorBg        = lipgloss.Color("#F5F5F7")
	colorPanel     = lipgloss.Color("#ECECEC")
	colorInputBg   = lipgloss.Color("#FFFFFF")
	colorMuted     = lipgloss.Color("#6B6B6B")
	colorComment   = lipgloss.Color("#8C8C8C")
	colorText      = lipgloss.Color("#1F1F1F")
	colorThinking  = lipgloss.Color("#D97706")
	colorCommand   = lipgloss.Color("#2563EB")
	colorPath      = lipgloss.Color("#059669")
	colorSuccess   = lipgloss.Color("#16A34A")
	colorError     = lipgloss.Color("#DC2626")
	colorWarning   = lipgloss.Color("#D97706")
	colorStatus    = lipgloss.Color("#7C3AED")
	colorDivider   = lipgloss.Color("#D1D5DB")
	colorAssistant = lipgloss.Color("#1F1F1F")
	colorCodeText  = lipgloss.Color("#2F2F2F")
	colorCodeBg    = lipgloss.Color("#F0F0F0")

	styleUserTag = lipgloss.NewStyle().Foreground(colorCommand).Bold(true)
	styleAITag   = lipgloss.NewStyle().Foreground(colorAssistant).Bold(true)
	styleSysTag  = lipgloss.NewStyle().Foreground(colorComment)
	styleToolTag = lipgloss.NewStyle().Foreground(colorCommand).Bold(true)

	styleBody     = lipgloss.NewStyle().Background(colorBg)
	styleSys      = lipgloss.NewStyle().Foreground(colorMuted).Background(colorBg)
	styleError    = lipgloss.NewStyle().Foreground(colorError).Background(colorBg).Bold(true)
	styleWarning  = lipgloss.NewStyle().Foreground(colorWarning).Background(colorBg).Bold(true)
	styleSuccess  = lipgloss.NewStyle().Foreground(colorSuccess).Background(colorBg).Bold(true)
	styleThinking = lipgloss.NewStyle().Foreground(colorThinking).Background(colorBg).Italic(true)

	styleText   = lipgloss.NewStyle().Foreground(colorText).Background(colorBg)
	styleCode   = lipgloss.NewStyle().Foreground(colorCodeText).Background(colorCodeBg)
	styleCmd    = lipgloss.NewStyle().Foreground(colorCommand).Background(colorBg)
	stylePath   = lipgloss.NewStyle().Foreground(colorPath).Background(colorBg)
	styleInline = lipgloss.NewStyle().Foreground(colorComment).Background(colorBg)

	styleHeaderBar   = lipgloss.NewStyle().Background(colorPanel)
	styleHeaderTitle = lipgloss.NewStyle().Foreground(colorText).Background(colorPanel).Bold(true)
	styleHeaderMuted = lipgloss.NewStyle().Foreground(colorMuted).Background(colorPanel)
	styleHelp        = lipgloss.NewStyle().Foreground(colorMuted).Background(colorPanel)

	styleInput        = lipgloss.NewStyle().Foreground(colorText).Background(colorInputBg)
	styleInputFocused = lipgloss.NewStyle().Foreground(colorText).Background(lipgloss.Color("#F9F9F9"))
	styleStatus       = lipgloss.NewStyle().Foreground(colorStatus).Background(colorPanel)
	styleBorderBody   = lipgloss.NewStyle().Foreground(colorDivider).Background(colorBg)
	styleBorderPanel  = lipgloss.NewStyle().Foreground(colorDivider).Background(colorPanel)
	styleBorderInput  = lipgloss.NewStyle().Foreground(colorDivider).Background(colorInputBg)
	styleBorderFocus  = lipgloss.NewStyle().Foreground(colorCommand).Background(colorInputBg).Bold(true)
	stylePrompt       = lipgloss.NewStyle().Foreground(colorCommand).Background(colorInputBg).Bold(true)
)

type outboundMsg struct {
	channel  string
	content  string
	err      error
	metadata map[string]string
}

type tuiModel struct {
	ctx                  context.Context
	cancel               context.CancelFunc
	bus                  *bus.MessageBus
	input                textinput.Model
	spin                 spinner.Model
	width                int
	height               int
	help                 bool
	thinking             bool
	messages             []tuiMessage
	scrollOffset         int
	autoFollow           bool
	lastSentAt           time.Time
	lastLatency          time.Duration
	lastError            string
	lastErrorCode        string
	lastPromptTokens     int
	lastCompletionTokens int
	lastTotalTokens      int
	lastCachedTokens     int
	provider             string
	model                string
	baseHost             string
	workspace            string
	restrictToWorkspace  bool
}

func runAgentTUI(ctx context.Context, cancel context.CancelFunc, b *bus.MessageBus, cfg config.Config, stdout, stderr io.Writer) int {
	m := newTUIModel(ctx, cancel, b, cfg)
	p := tea.NewProgram(m, tea.WithOutput(stdout), tea.WithAltScreen())
	if err := p.Start(); err != nil {
		fmt.Fprintf(stderr, "tui error: %v\n", err)
		return 1
	}
	return 0
}

func newTUIModel(ctx context.Context, cancel context.CancelFunc, b *bus.MessageBus, cfg config.Config) tuiModel {
	ti := textinput.New()
	ti.Placeholder = "Message..."
	ti.Prompt = ""
	ti.TextStyle = lipgloss.NewStyle().Foreground(colorText)
	ti.Focus()
	ti.CharLimit = 4096

	sp := spinner.New()
	sp.Spinner = spinner.MiniDot

	m := tuiModel{
		ctx:        ctx,
		cancel:     cancel,
		bus:        b,
		input:      ti,
		spin:       sp,
		help:       false,
		autoFollow: true,
	}
	if wd, err := os.Getwd(); err == nil && wd != "" {
		m.workspace = filepath.Base(wd)
	} else {
		m.workspace = "workspace"
	}
	m.restrictToWorkspace = cfg.Tools.RestrictToWorkspace

	provider, model, baseURL, apiKey := configSummary(cfg)
	m.provider = provider
	m.model = model
	m.baseHost = baseURLHost(baseURL)
	_ = apiKey
	m.appendSystem("Ready. Type /help for commands.")
	return m
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(waitOutboundCmd(m.ctx, m.bus), m.spin.Tick)
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.cancel()
			return m, tea.Quit
		case "up":
			m.scrollOffset = clamp(m.scrollOffset+1, 0, max(0, m.maxScrollOffset()))
			m.autoFollow = m.scrollOffset == 0
			return m, nil
		case "down":
			m.scrollOffset = clamp(m.scrollOffset-1, 0, max(0, m.maxScrollOffset()))
			m.autoFollow = m.scrollOffset == 0
			return m, nil
		case "pgup":
			m.scrollOffset = clamp(m.scrollOffset+5, 0, max(0, m.maxScrollOffset()))
			m.autoFollow = m.scrollOffset == 0
			return m, nil
		case "pgdown":
			m.scrollOffset = clamp(m.scrollOffset-5, 0, max(0, m.maxScrollOffset()))
			m.autoFollow = m.scrollOffset == 0
			return m, nil
		case "home":
			m.scrollOffset = m.maxScrollOffset()
			m.autoFollow = false
			return m, nil
		case "end":
			m.scrollOffset = 0
			m.autoFollow = true
			return m, nil
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			if text == "" {
				return m, nil
			}
			m.input.SetValue("")
			switch strings.ToLower(text) {
			case "/help":
				m.help = !m.help
				return m, nil
			case "/clear":
				m.messages = nil
				m.appendSystem("cleared")
				return m, nil
			case "/exit", "/quit":
				m.cancel()
				return m, tea.Quit
			default:
				m.appendUser(text)
				m.thinking = true
				m.lastSentAt = time.Now()
				m.lastError = ""
				if m.autoFollow {
					m.scrollOffset = 0
				}
				_ = m.bus.PublishInbound(bus.InboundMessage{
					Channel: "cli",
					ChatID:  "console",
					Content: text,
				})
				return m, nil
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = max(10, m.width-10)
	case outboundMsg:
		if msg.err != nil {
			if errors.Is(msg.err, context.Canceled) {
				return m, tea.Quit
			}
			m.lastError = msg.err.Error()
			m.lastErrorCode = extractStatusCode(msg.err.Error())
			m.appendSystem("error: " + msg.err.Error())
			m.thinking = false
			return m, waitOutboundCmd(m.ctx, m.bus)
		}
		if msg.channel == "cli" {
			m.appendAssistant(msg.content)
			if usageLine := formatUsageLine(msg.metadata); usageLine != "" {
				m.appendSystem(usageLine)
			}
			if !m.lastSentAt.IsZero() {
				m.lastLatency = time.Since(m.lastSentAt)
			}
			m.thinking = false
			if m.autoFollow {
				m.scrollOffset = 0
			}
			if msg.metadata != nil {
				if code, ok := msg.metadata["error_code"]; ok {
					m.lastErrorCode = code
				}
				if total, ok := msg.metadata["total_tokens"]; ok {
					m.lastTotalTokens = parseInt(total)
				}
				if pt, ok := msg.metadata["prompt_tokens"]; ok {
					m.lastPromptTokens = parseInt(pt)
				}
				if ct, ok := msg.metadata["completion_tokens"]; ok {
					m.lastCompletionTokens = parseInt(ct)
				}
				if cached, ok := msg.metadata["cached_tokens"]; ok {
					m.lastCachedTokens = parseInt(cached)
				}
				if ms, ok := msg.metadata["latency_ms"]; ok {
					if parsed := parseMillis(ms); parsed > 0 {
						m.lastLatency = time.Duration(parsed) * time.Millisecond
					}
				}
			}
		}
		return m, waitOutboundCmd(m.ctx, m.bus)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m tuiModel) View() string {
	var b strings.Builder
	header := renderFullLine(styleHeaderBar, m.headerLine(), m.width)
	b.WriteString(header)
	b.WriteString("\n")
	headerLines := 1
	if m.help {
		b.WriteString(renderFullLine(styleHelp, m.helpLine(), m.width))
		b.WriteString("\n")
		headerLines++
	}

	bodyLines := renderMessages(m.messages, max(10, m.width-2))
	if m.thinking {
		bodyLines = append(bodyLines, styleThinking.Render("Thinking "+m.spin.View()))
	}
	available := m.bodyHeight(headerLines)
	if len(bodyLines) > available {
		offset := clamp(m.scrollOffset, 0, max(0, len(bodyLines)-available))
		start := max(0, len(bodyLines)-available-offset)
		bodyLines = bodyLines[start : start+available]
	}
	for _, line := range bodyLines {
		b.WriteString(renderWithBorder(styleBody, line, m.width))
		b.WriteString("\n")
	}
	b.WriteString(renderWithBorderWithStyle(styleBorderPanel, styleStatus, m.statusLine(), m.width))
	b.WriteString("\n")
	inputStyle := styleInput
	if m.input.Focused() {
		inputStyle = styleInputFocused
	}
	borderStyle := styleBorderInput
	if m.input.Focused() {
		borderStyle = styleBorderFocus
	}
	prompt := stylePrompt.Render("❯ ")
	b.WriteString(renderWithBorderWithStyle(borderStyle, inputStyle, prompt+m.input.View(), m.width))
	return b.String()
}

func waitOutboundCmd(ctx context.Context, b *bus.MessageBus) tea.Cmd {
	return func() tea.Msg {
		msg, err := b.ConsumeOutbound(ctx)
		if err != nil {
			return outboundMsg{err: err}
		}
		return outboundMsg{channel: msg.Channel, content: msg.Content, metadata: msg.Metadata}
	}
}

func (m *tuiModel) appendSystem(text string) {
	m.messages = append(m.messages, tuiMessage{role: "sys", content: text})
}

func (m *tuiModel) appendUser(text string) {
	m.messages = append(m.messages, tuiMessage{role: "you", content: text})
}

func (m *tuiModel) appendAssistant(text string) {
	m.messages = append(m.messages, tuiMessage{role: "ai", content: text})
}

func (m tuiModel) maxScrollOffset() int {
	bodyLines := renderMessages(m.messages, max(10, m.width-2))
	headerLines := 1
	if m.help {
		headerLines++
	}
	available := m.bodyHeight(headerLines)
	if len(bodyLines) <= available {
		return 0
	}
	return len(bodyLines) - available
}

func renderMessages(msgs []tuiMessage, width int) []string {
	var out []string
	for _, msg := range msgs {
		tag := "SYS"
		tagStyle := styleSysTag
		contentStyle := styleSys
		switch msg.role {
		case "you":
			tag = "YOU"
			tagStyle = styleUserTag
			contentStyle = styleText
		case "ai":
			tag = "AI"
			tagStyle = styleAITag
			contentStyle = styleText
		case "sys":
			lower := strings.ToLower(strings.TrimSpace(msg.content))
			if strings.HasPrefix(msg.content, "error:") || strings.HasPrefix(lower, "error") {
				tag = "ERR"
				tagStyle = styleError
				contentStyle = styleError
			} else if strings.HasPrefix(lower, "tool") {
				tag = "TOOL"
				tagStyle = styleToolTag
				contentStyle = styleToolTag
			} else if strings.HasPrefix(lower, "thinking:") {
				tag = "THINK"
				tagStyle = styleThinking
				contentStyle = styleThinking
			} else {
				tag = "SYS"
				tagStyle = styleSysTag
				contentStyle = styleSys
			}
		}
		prefix := tagStyle.Render(tag) + " "
		prefixWidth := lipgloss.Width(prefix)
		spacer := strings.Repeat(" ", prefixWidth)
		firstLine := true
		inCode := false
		for _, rawLine := range splitLines(msg.content) {
			trim := strings.TrimSpace(rawLine)
			if strings.HasPrefix(trim, "```") {
				inCode = !inCode
				continue
			}
			lineStyle := contentStyle
			if inCode {
				lineStyle = styleCode
			} else {
				switch {
				case strings.HasPrefix(trim, "✔") || strings.HasPrefix(strings.ToLower(trim), "ok"):
					lineStyle = styleSuccess
				case strings.HasPrefix(trim, "⚠") || strings.HasPrefix(strings.ToLower(trim), "warn"):
					lineStyle = styleWarning
				case strings.HasPrefix(trim, "✖") || strings.HasPrefix(strings.ToLower(trim), "error"):
					lineStyle = styleError
				case strings.HasPrefix(trim, "$ ") || strings.HasPrefix(trim, "> ") || strings.HasPrefix(trim, "cmd:") || strings.HasPrefix(trim, "command:"):
					lineStyle = styleCmd
				case isPathLine(trim):
					lineStyle = stylePath
				}
			}

			var lines []string
			if inCode {
				lines = wrapTextRaw(rawLine, max(10, width-prefixWidth))
			} else {
				lines = wrapText(rawLine, max(10, width-prefixWidth))
			}
			for i, line := range lines {
				rendered := lineStyle.Render(line)
				renderPrefix := spacer
				if firstLine {
					renderPrefix = prefix
				}
				if i == 0 && !firstLine {
					renderPrefix = spacer
				}
				out = append(out, renderPrefix+rendered)
				firstLine = false
			}
		}
	}
	return out
}

func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) > width {
			lines = append(lines, line)
			line = w
			continue
		}
		line += " " + w
	}
	lines = append(lines, line)
	return lines
}

func wrapTextRaw(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	if text == "" {
		return []string{""}
	}
	var out []string
	var buf []rune
	for _, r := range text {
		buf = append(buf, r)
		if len(buf) >= width {
			out = append(out, string(buf))
			buf = buf[:0]
		}
	}
	if len(buf) > 0 {
		out = append(out, string(buf))
	}
	return out
}

func splitLines(text string) []string {
	if text == "" {
		return []string{""}
	}
	return strings.Split(text, "\n")
}

func isPathLine(line string) bool {
	if line == "" {
		return false
	}
	if strings.HasPrefix(line, "./") || strings.HasPrefix(line, "../") || strings.HasPrefix(line, "/") {
		return true
	}
	if strings.HasSuffix(line, "/") && !strings.Contains(line, " ") {
		return true
	}
	if strings.Contains(line, "/") && !strings.Contains(line, " ") && len(line) <= 80 {
		return true
	}
	return false
}

func configSummary(cfg config.Config) (string, string, string, string) {
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
	return providerName, model, baseURL, apiKey
}

func configSummaryLines(provider, model, baseURL, apiKey string) []string {
	return []string{
		fmt.Sprintf("config: provider=%s model=%s", provider, model),
		fmt.Sprintf("config: base_url=%s", baseURL),
		fmt.Sprintf("config: api_key=%s", apiKey),
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func clamp(v, minV, maxV int) int {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func renderWithBorderWithStyle(borderStyle, contentStyle lipgloss.Style, line string, width int) string {
	if width <= 0 {
		return borderStyle.Render("│ ") + contentStyle.Render(line)
	}
	prefix := borderStyle.Render("│ ")
	contentWidth := width - lipgloss.Width(prefix)
	if contentWidth < 0 {
		contentWidth = 0
	}
	padding := contentWidth - lipgloss.Width(line)
	if padding < 0 {
		padding = 0
	}
	return prefix + contentStyle.Render(line+strings.Repeat(" ", padding))
}

func renderWithBorder(style lipgloss.Style, line string, width int) string {
	return renderWithBorderWithStyle(styleBorderBody, style, line, width)
}

func parseInt(v string) int {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	n := 0
	for _, r := range v {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func parseMillis(v string) int {
	return parseInt(v)
}

func extractStatusCode(errMsg string) string {
	idx := strings.Index(errMsg, "openai status ")
	if idx == -1 {
		return ""
	}
	part := errMsg[idx+len("openai status "):]
	fields := strings.Fields(part)
	if len(fields) == 0 {
		return ""
	}
	code := fields[0]
	for _, r := range code {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return code
}

func formatUsageLine(meta map[string]string) string {
	if len(meta) == 0 {
		return ""
	}
	total := parseInt(meta["total_tokens"])
	if total == 0 {
		total = parseInt(meta["prompt_tokens"]) + parseInt(meta["completion_tokens"])
	}
	cached := parseInt(meta["cached_tokens"])
	notHit := "-"
	if total > 0 {
		net := total - cached
		if net < 0 {
			net = 0
		}
		notHit = fmt.Sprintf("%d", net)
	}
	hit := fmt.Sprintf("%d", cached)
	usetime := strings.TrimSpace(meta["latency_ms"])
	if usetime == "" {
		usetime = "-"
	} else if !strings.HasSuffix(usetime, "ms") {
		usetime = usetime + "ms"
	}
	if total == 0 && usetime == "-" && cached == 0 {
		return ""
	}
	return fmt.Sprintf("hit_cache %s | not_hit %s | usetime %s", hit, notHit, usetime)
}

func baseURLHost(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" || baseURL == "(default)" {
		return "(default)"
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return baseURL
	}
	return u.Host
}

func (m tuiModel) statusLine() string {
	errCode := ""
	if m.lastErrorCode != "" {
		errCode = " err:" + m.lastErrorCode
	}
	scroll := ""
	if maxScroll := m.maxScrollOffset(); maxScroll > 0 {
		scroll = fmt.Sprintf(" scroll:%d/%d", m.maxScrollOffset()-m.scrollOffset, m.maxScrollOffset())
	}
	right := fmt.Sprintf("[Enter] Send  [Ctrl+C] Cancel  [/command]")
	space := max(1, m.width-lipgloss.Width(right)-2)
	return strings.Repeat(" ", space) + right + errCode + scroll
}

func (m tuiModel) headerLine() string {
	sandbox := "full"
	if m.restrictToWorkspace {
		sandbox = "workspace-write"
	}
	left := styleHeaderTitle.Render("lobster-go") + styleHeaderMuted.Render(" | Model: "+m.model+" | Sandbox: "+sandbox+" | Session: active")
	right := styleHeaderMuted.Render("Project: " + m.workspace)
	space := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", space) + right
}

func (m tuiModel) helpLine() string {
	return "Shortcuts: /help  /clear  /exit  •  PgUp/PgDn  Home/End  •  Ctrl+C"
}

func (m tuiModel) bodyHeight(headerLines int) int {
	available := m.height - headerLines - 2
	if available < 3 {
		return 3
	}
	return available
}

func renderFullLine(style lipgloss.Style, line string, width int) string {
	if width <= 0 {
		return style.Render(line)
	}
	padding := width - lipgloss.Width(line)
	if padding < 0 {
		padding = 0
	}
	return style.Render(line + strings.Repeat(" ", padding))
}

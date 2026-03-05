package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
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
	colorAccent  = lipgloss.Color("#7C3AED")
	colorText    = lipgloss.Color("#1F1F1F")
	colorMuted   = lipgloss.Color("#8C8C8C")
	colorBg      = lipgloss.Color("#F7F7F5")
	colorInputBg = lipgloss.Color("#FDFDFD")
	colorStatus  = lipgloss.Color("#F1F1EF")

	styleUser         = lipgloss.NewStyle().Foreground(lipgloss.Color("#4C9F38")).Bold(true)
	styleAI           = lipgloss.NewStyle().Foreground(lipgloss.Color("#1F78B4")).Bold(true)
	styleSys          = lipgloss.NewStyle().Foreground(colorMuted)
	styleError        = lipgloss.NewStyle().Foreground(lipgloss.Color("#D32F2F")).Bold(true)
	styleThinking     = lipgloss.NewStyle().Foreground(lipgloss.Color("#B08968")).Italic(true)
	styleInput        = lipgloss.NewStyle().Foreground(colorText).Background(colorInputBg)
	styleInputFocused = lipgloss.NewStyle().Foreground(colorText).Background(lipgloss.Color("#FFFFFF"))
	styleBody         = lipgloss.NewStyle().Foreground(colorText).Background(colorBg)
	styleStatus       = lipgloss.NewStyle().Foreground(colorMuted).Background(colorStatus)
	styleBorder       = lipgloss.NewStyle().Foreground(colorAccent)
	styleBorderFocus  = lipgloss.NewStyle().Foreground(lipgloss.Color("#5B21B6")).Bold(true)
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
	provider             string
	model                string
	baseHost             string
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
	ti.Placeholder = "Type a message..."
	ti.Prompt = ""
	ti.TextStyle = lipgloss.NewStyle().Foreground(colorText)
	ti.Focus()
	ti.CharLimit = 4096

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	m := tuiModel{
		ctx:        ctx,
		cancel:     cancel,
		bus:        b,
		input:      ti,
		spin:       sp,
		help:       false,
		autoFollow: true,
	}

	provider, model, baseURL, apiKey := configSummary(cfg)
	m.provider = provider
	m.model = model
	m.baseHost = baseURLHost(baseURL)
	m.appendSystem("commands: /help /clear /exit")
	m.appendSystem(fmt.Sprintf("config: provider=%s model=%s", provider, model))
	m.appendSystem(fmt.Sprintf("config: base_url=%s", baseURL))
	m.appendSystem(fmt.Sprintf("config: api_key=%s", apiKey))
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
		m.input.Width = max(10, m.width-8)
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
	bodyLines := renderMessages(m.messages, max(10, m.width-2))
	if m.thinking {
		bodyLines = append(bodyLines, styleThinking.Render("Thinking: "+m.spin.View()))
	}
	available := m.height - 3
	if available < 3 {
		available = 3
	}
	if len(bodyLines) > available {
		offset := clamp(m.scrollOffset, 0, max(0, len(bodyLines)-available))
		start := max(0, len(bodyLines)-available-offset)
		bodyLines = bodyLines[start : start+available]
	}
	for _, line := range bodyLines {
		b.WriteString(renderWithBorder(styleBody, line, m.width))
		b.WriteString("\n")
	}
	b.WriteString(renderWithBorder(styleStatus, m.statusLine(), m.width))
	b.WriteString("\n")
	inputStyle := styleInput
	if m.input.Focused() {
		inputStyle = styleInputFocused
	}
	borderStyle := styleBorder
	if m.input.Focused() {
		borderStyle = styleBorderFocus
	}
	b.WriteString(renderWithBorderWithStyle(borderStyle, inputStyle, m.input.View(), m.width))
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
	available := m.height - 4
	if available < 3 {
		available = 3
	}
	if len(bodyLines) <= available {
		return 0
	}
	return len(bodyLines) - available
}

func renderMessages(msgs []tuiMessage, width int) []string {
	var out []string
	for _, msg := range msgs {
		prefix := msg.role + " > "
		prefixStyle := styleSys
		switch msg.role {
		case "you":
			prefixStyle = styleUser
		case "ai":
			prefixStyle = styleAI
		case "sys":
			if strings.HasPrefix(msg.content, "error:") {
				prefixStyle = styleError
			} else if strings.HasPrefix(strings.ToLower(msg.content), "thinking:") {
				prefixStyle = styleThinking
			} else {
				prefixStyle = styleSys
			}
		}
		lines := wrapText(msg.content, max(10, width-len(prefix)))
		for i, line := range lines {
			if i == 0 {
				out = append(out, prefixStyle.Render(prefix)+line)
			} else {
				out = append(out, strings.Repeat(" ", len(prefix))+line)
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
	return renderWithBorderWithStyle(styleBorder, style, line, width)
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

func latencyBucket(d time.Duration) string {
	switch {
	case d < time.Second:
		return "<1s"
	case d < 3*time.Second:
		return "1-3s"
	case d < 10*time.Second:
		return "3-10s"
	default:
		return ">10s"
	}
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
	thinking := ""
	if m.thinking {
		thinking = " " + styleThinking.Render("Thinking:") + " " + styleThinking.Render(m.spin.View())
	}
	latency := "-"
	if m.lastLatency > 0 {
		latency = latencyBucket(m.lastLatency)
	}
	tokens := "-"
	if m.lastTotalTokens > 0 {
		tokens = fmt.Sprintf("%d", m.lastTotalTokens)
	}
	errCode := ""
	if m.lastErrorCode != "" {
		errCode = " err:" + m.lastErrorCode
	}
	scroll := ""
	if maxScroll := m.maxScrollOffset(); maxScroll > 0 {
		scroll = fmt.Sprintf(" scroll:%d/%d", m.maxScrollOffset()-m.scrollOffset, m.maxScrollOffset())
	}
	left := fmt.Sprintf("Build %s • %s • tok:%s", m.model, latency, tokens)
	right := fmt.Sprintf("ctrl+t variants  tab agents  ctrl+p commands")
	space := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(right)-4)
	return left + strings.Repeat(" ", space) + right + errCode + thinking + scroll
}

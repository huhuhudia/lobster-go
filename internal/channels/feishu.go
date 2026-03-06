package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	larkdispatcher "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/huhuhudia/lobster-go/internal/bus"
	"github.com/huhuhudia/lobster-go/pkg/logging"
)

// FeishuConfig holds Feishu/Lark channel configuration.
type FeishuConfig struct {
	Enabled           bool     `json:"enabled"`
	AppID             string   `json:"appId"`
	AppSecret         string   `json:"appSecret"`
	EncryptKey        string   `json:"encryptKey,omitempty"`
	VerificationToken string   `json:"verificationToken,omitempty"`
	UseWebhook        bool     `json:"useWebhook,omitempty"`
	UseCard           bool     `json:"useCard,omitempty"`
	AllowFrom         []string `json:"allowFrom,omitempty"`
	ReactEmoji        string   `json:"reactEmoji,omitempty"`
}

// FeishuChannel implements the Feishu/Lark bot using WebSocket long connection.
type FeishuChannel struct {
	BaseChannel
	Config            FeishuConfig
	tenantAccessToken string
	tokenExpiry       time.Time
	tokenMutex        sync.Mutex
	httpClient        *http.Client
	processedMsgIDs   map[string]time.Time
	processedMutex    sync.Mutex
	wsClient          *larkws.Client
}

// NewFeishuChannel creates a new Feishu channel.
func NewFeishuChannel(cfg FeishuConfig, b *bus.MessageBus) *FeishuChannel {
	return &FeishuChannel{
		BaseChannel: BaseChannel{
			Name: "feishu",
			Bus:  b,
		},
		Config:          cfg,
		httpClient:      &http.Client{Timeout: 30 * time.Second},
		processedMsgIDs: make(map[string]time.Time),
	}
}

// Start begins the Feishu bot with WebSocket long connection.
func (f *FeishuChannel) Start(ctx context.Context) error {
	if !f.Config.Enabled {
		logging.Default.Info("Feishu channel is disabled")
		return nil
	}

	if f.Config.AppID == "" || f.Config.AppSecret == "" {
		return fmt.Errorf("feishu app_id and app_secret are required")
	}

	f.Running = true
	if f.Config.UseWebhook {
		logging.Default.Info("Feishu bot starting in webhook mode (no websocket)")
	} else {
		logging.Default.Info("Feishu bot starting with WebSocket long connection")
	}

	// Start token refresh goroutine (used for outbound API calls)
	go f.tokenRefreshLoop(ctx)

	// Start message dedup cleanup
	go f.cleanupProcessedMessages(ctx)

	if !f.Config.UseWebhook {
		dispatcher := larkdispatcher.NewEventDispatcher(
			f.Config.VerificationToken,
			f.Config.EncryptKey,
		).OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			return f.handleMessageEventV1(ctx, event)
		})

		client := larkws.NewClient(
			f.Config.AppID,
			f.Config.AppSecret,
			larkws.WithEventHandler(dispatcher),
		)
		f.wsClient = client

		go func() {
			if err := client.Start(ctx); err != nil {
				logging.Default.Error("feishu ws client stopped: %v", err)
			}
		}()
	}

	// Keep running until context cancelled
	<-ctx.Done()
	f.Running = false
	logging.Default.Info("Feishu bot stopped")
	return nil
}

// Stop stops the Feishu bot.
func (f *FeishuChannel) Stop() error {
	f.Running = false
	return nil
}

// Send sends a message through Feishu.
func (f *FeishuChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !f.Running {
		return fmt.Errorf("feishu channel not running")
	}

	content := msg.Content
	if suffix := formatUsageSuffix(msg.Metadata); suffix != "" {
		if strings.TrimSpace(content) == "" {
			content = suffix
		} else {
			content = content + "\n\n" + suffix
		}
	}

	token, err := f.getTenantAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("get access token: %w", err)
	}

	// Determine receive_id_type based on chat_id format
	receiveIDType := "chat_id"
	if !strings.HasPrefix(msg.ChatID, "oc_") {
		receiveIDType = "open_id"
	}

	// Send media files first
	for _, filePath := range msg.Media {
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			logging.Default.Warn("media file not found: %s", filePath)
			continue
		}
		if err := f.sendFile(ctx, token, receiveIDType, msg.ChatID, filePath); err != nil {
			logging.Default.Error("send media failed: %v", err)
		}
	}

	// Send text content as interactive card (optional), fallback to plain text.
	if content != "" {
		if f.Config.UseCard {
			card := f.buildCard(content)
			if err := f.sendMessage(ctx, token, receiveIDType, msg.ChatID, "interactive", card); err != nil {
				logging.Default.Warn("feishu card send failed, fallback to text: %v", err)
				if err := f.sendMessage(ctx, token, receiveIDType, msg.ChatID, "text", map[string]string{"text": content}); err != nil {
					return fmt.Errorf("send message: %w", err)
				}
			}
		} else {
			if err := f.sendMessage(ctx, token, receiveIDType, msg.ChatID, "text", map[string]string{"text": content}); err != nil {
				return fmt.Errorf("send message: %w", err)
			}
		}
	}

	return nil
}

// tokenRefreshLoop periodically refreshes the tenant access token.
func (f *FeishuChannel) tokenRefreshLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Minute):
			_, err := f.getTenantAccessToken(ctx)
			if err != nil {
				logging.Default.Error("refresh token failed: %v", err)
			}
		}
	}
}

// getTenantAccessToken gets a fresh tenant access token.
func (f *FeishuChannel) getTenantAccessToken(ctx context.Context) (string, error) {
	f.tokenMutex.Lock()
	defer f.tokenMutex.Unlock()

	// Return cached token if still valid
	if f.tenantAccessToken != "" && time.Now().Before(f.tokenExpiry.Add(-5*time.Minute)) {
		return f.tenantAccessToken, nil
	}

	// Request new token
	reqBody := map[string]string{
		"app_id":     f.Config.AppID,
		"app_secret": f.Config.AppSecret,
	}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if result.Code != 0 {
		return "", fmt.Errorf("feishu auth error: code=%d msg=%s", result.Code, result.Msg)
	}

	f.tenantAccessToken = result.TenantAccessToken
	f.tokenExpiry = time.Now().Add(time.Duration(result.Expire) * time.Second)

	return f.tenantAccessToken, nil
}

// sendMessage sends a message through Feishu API.
func (f *FeishuChannel) sendMessage(ctx context.Context, token, receiveIDType, receiveID, msgType string, content interface{}) error {
	if strings.TrimSpace(receiveIDType) == "" {
		receiveIDType = "open_id"
	}
	if strings.TrimSpace(receiveID) == "" {
		return fmt.Errorf("receive_id is required")
	}

	normalized := content
	if msgType == "interactive" {
		if m, ok := content.(map[string]interface{}); ok {
			if _, exists := m["card"]; !exists {
				normalized = map[string]interface{}{"card": m}
			}
		}
	}
	contentJSON, _ := json.Marshal(normalized)

	reqBody := map[string]interface{}{
		"receive_id": receiveID,
		"msg_type":   msgType,
		"content":    string(contentJSON),
	}
	body, _ := json.Marshal(reqBody)

	endpoint := "https://open.feishu.cn/open-apis/im/v1/messages?receive_id_type=" + url.QueryEscape(receiveIDType)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	if result.Code != 0 {
		return fmt.Errorf("feishu send error: code=%d msg=%s", result.Code, result.Msg)
	}

	return nil
}

// sendFile uploads and sends a file.
func (f *FeishuChannel) sendFile(ctx context.Context, token, receiveIDType, receiveID, filePath string) error {
	// Read file
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(filePath))
	imageExts := map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".bmp": true, ".webp": true}

	if imageExts[ext] {
		// Upload image
		imageKey, err := f.uploadImage(ctx, token, filePath)
		if err != nil {
			return err
		}
		content := map[string]string{"image_key": imageKey}
		return f.sendMessage(ctx, token, receiveIDType, receiveID, "image", content)
	}

	// Upload file
	fileKey, err := f.uploadFile(ctx, token, filePath)
	if err != nil {
		return err
	}
	fileType := "stream"
	if ext == ".opus" {
		fileType = "opus"
	}
	content := map[string]string{"file_key": fileKey}
	return f.sendMessage(ctx, token, receiveIDType, receiveID, fileType, content)
}

// uploadImage uploads an image to Feishu.
func (f *FeishuChannel) uploadImage(ctx context.Context, token, filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// Create multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("image_type", "message")
	part, _ := writer.CreateFormFile("image", filepath.Base(filePath))
	io.Copy(part, file)
	writer.Close()

	req, _ := http.NewRequestWithContext(ctx, "POST", "https://open.feishu.cn/open-apis/im/v1/images", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Code int `json:"code"`
		Data struct {
			ImageKey string `json:"image_key"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if result.Code != 0 {
		return "", fmt.Errorf("upload image failed: code=%d", result.Code)
	}

	return result.Data.ImageKey, nil
}

// uploadFile uploads a file to Feishu.
func (f *FeishuChannel) uploadFile(ctx context.Context, token, filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("file_type", "stream")
	_ = writer.WriteField("file_name", filepath.Base(filePath))
	part, _ := writer.CreateFormFile("file", filepath.Base(filePath))
	io.Copy(part, file)
	writer.Close()

	req, _ := http.NewRequestWithContext(ctx, "POST", "https://open.feishu.cn/open-apis/im/v1/files", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Code int `json:"code"`
		Data struct {
			FileKey string `json:"file_key"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if result.Code != 0 {
		return "", fmt.Errorf("upload file failed: code=%d", result.Code)
	}

	return result.Data.FileKey, nil
}

// buildCard creates a Feishu interactive card from content.
func (f *FeishuChannel) buildCard(content string) map[string]interface{} {
	elements := f.buildCardElements(content)
	return map[string]interface{}{
		"config": map[string]interface{}{
			"wide_screen_mode": true,
		},
		"header": map[string]interface{}{
			"title": map[string]interface{}{
				"tag":     "plain_text",
				"content": "lobster-go",
			},
		},
		"elements": elements,
	}
}

// buildCardElements splits content into card elements.
func (f *FeishuChannel) buildCardElements(content string) []map[string]interface{} {
	// Simple implementation: single markdown block
	// TODO: Add table parsing like Python version
	return []map[string]interface{}{
		{
			"tag": "div",
			"text": map[string]interface{}{
				"tag":     "lark_md",
				"content": content,
			},
		},
	}
}

// HandleWebhookEvent processes an incoming webhook event from Feishu.
func (f *FeishuChannel) HandleWebhookEvent(ctx context.Context, event json.RawMessage) error {
	var evt struct {
		Schema string `json:"schema"`
		Header struct {
			EventID    string `json:"event_id"`
			EventType  string `json:"event_type"`
			CreateTime string `json:"create_time"`
			Token      string `json:"token"`
		} `json:"header"`
		Event json.RawMessage `json:"event"`
	}

	if err := json.Unmarshal(event, &evt); err != nil {
		return err
	}

	// Verify token if configured
	if f.Config.VerificationToken != "" && evt.Header.Token != f.Config.VerificationToken {
		return fmt.Errorf("invalid verification token")
	}

	// Handle message receive event
	if evt.Header.EventType == "im.message.receive_v1" {
		return f.handleMessageEvent(ctx, evt.Event)
	}

	return nil
}

// handleMessageEventV1 processes a message receive event from WebSocket.
func (f *FeishuChannel) handleMessageEventV1(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	if event == nil || event.Event == nil || event.Event.Message == nil || event.Event.Sender == nil {
		return nil
	}

	senderType := derefString(event.Event.Sender.SenderType)
	if senderType != "" && senderType != "user" {
		return nil
	}

	senderID := extractSenderID(event.Event.Sender)
	if senderID == "" {
		return nil
	}

	// Check permission
	if !f.IsAllowed(f.Config.AllowFrom, senderID) {
		logging.Default.Warn("access denied for sender %s", senderID)
		return nil
	}

	msg := event.Event.Message
	msgID := derefString(msg.MessageId)
	if msgID != "" && f.isProcessed(msgID) {
		return nil
	}
	if msgID != "" {
		f.markProcessed(msgID)
	}

	msgType := derefString(msg.MessageType)
	content := derefString(msg.Content)
	chatID := derefString(msg.ChatId)
	chatType := derefString(msg.ChatType)

	text, media := f.parseMessageContent(msgType, content)
	if text == "" && len(media) == 0 {
		return nil
	}

	if emoji := f.reactionEmoji(); emoji != "" && msgID != "" {
		go f.reactMessage(context.Background(), msgID, emoji)
	}

	if chatType == "p2p" {
		chatID = senderID
	}

	return f.PublishInbound(ctx, senderID, chatID, text, media, map[string]string{
		"message_id": msgID,
		"chat_type":  chatType,
		"msg_type":   msgType,
	})
}

// handleMessageEvent processes a message receive event.
func (f *FeishuChannel) handleMessageEvent(ctx context.Context, eventData json.RawMessage) error {
	var evt struct {
		Sender struct {
			SenderID struct {
				OpenID string `json:"open_id"`
			} `json:"sender_id"`
			SenderType string `json:"sender_type"`
		} `json:"sender"`
		Message struct {
			MessageID   string `json:"message_id"`
			ChatID      string `json:"chat_id"`
			ChatType    string `json:"chat_type"`
			MessageType string `json:"message_type"`
			Content     string `json:"content"`
		} `json:"message"`
	}

	if err := json.Unmarshal(eventData, &evt); err != nil {
		return err
	}

	// Deduplication
	msgID := evt.Message.MessageID
	if f.isProcessed(msgID) {
		return nil
	}
	f.markProcessed(msgID)

	// Skip bot messages
	if evt.Sender.SenderType == "bot" {
		return nil
	}

	// Check permission
	senderID := evt.Sender.SenderID.OpenID
	if !f.IsAllowed(f.Config.AllowFrom, senderID) {
		logging.Default.Warn("access denied for sender %s", senderID)
		return nil
	}

	// Parse content
	content, media := f.parseMessageContent(evt.Message.MessageType, evt.Message.Content)

	if content == "" && len(media) == 0 {
		return nil
	}

	// Determine reply target
	chatID := evt.Message.ChatID
	if evt.Message.ChatType == "p2p" {
		chatID = senderID
	}

	if emoji := f.reactionEmoji(); emoji != "" && msgID != "" {
		go f.reactMessage(context.Background(), msgID, emoji)
	}

	// Publish to bus
	return f.PublishInbound(ctx, senderID, chatID, content, media, map[string]string{
		"message_id": msgID,
		"chat_type":  evt.Message.ChatType,
		"msg_type":   evt.Message.MessageType,
	})
}

func (f *FeishuChannel) reactMessage(ctx context.Context, messageID, emojiType string) {
	if strings.TrimSpace(messageID) == "" || strings.TrimSpace(emojiType) == "" {
		return
	}
	logging.Default.Info("feishu react: message_id=%s emoji=%s", messageID, emojiType)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	token, err := f.getTenantAccessToken(ctx)
	if err != nil {
		logging.Default.Warn("feishu react get token failed: %v", err)
		return
	}

	body, _ := json.Marshal(map[string]interface{}{
		"reaction_type": map[string]interface{}{
			"emoji_type": emojiType,
		},
	})
	endpoint := fmt.Sprintf("https://open.feishu.cn/open-apis/im/v1/messages/%s/reactions", url.PathEscape(messageID))
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		logging.Default.Warn("feishu react build request failed: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		logging.Default.Warn("feishu react request failed: %v", err)
		return
	}
	defer resp.Body.Close()

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		logging.Default.Warn("feishu react decode failed: %v", err)
		return
	}
	if result.Code != 0 {
		logging.Default.Warn("feishu react error: code=%d msg=%s", result.Code, result.Msg)
	}
}

func formatUsageSuffix(meta map[string]string) string {
	if len(meta) == 0 {
		return ""
	}
	tokens := strings.TrimSpace(meta["total_tokens"])
	if tokens == "" {
		prompt := strings.TrimSpace(meta["prompt_tokens"])
		complete := strings.TrimSpace(meta["completion_tokens"])
		if prompt != "" || complete != "" {
			if prompt == "" {
				prompt = "0"
			}
			if complete == "" {
				complete = "0"
			}
			tokens = prompt + "+" + complete
		}
	}
	cached := strings.TrimSpace(meta["cached_tokens"])
	usetime := strings.TrimSpace(meta["latency_ms"])
	if tokens == "" && usetime == "" {
		return ""
	}
	if usetime != "" && !strings.HasSuffix(usetime, "ms") {
		usetime = usetime + "ms"
	}
	if tokens == "" {
		tokens = "-"
	}
	if cached == "" {
		cached = "0"
	}
	netTokens := tokens
	if total, err := strconv.Atoi(tokens); err == nil {
		if hit, err := strconv.Atoi(cached); err == nil {
			if hit < 0 {
				hit = 0
			}
			if hit > total {
				hit = total
			}
			netTokens = strconv.Itoa(total - hit)
		}
	}
	if usetime == "" {
		usetime = "-"
	}
	return "hit_cache " + cached + " | not_hit " + netTokens + " | usetime " + usetime
}

func (f *FeishuChannel) reactionEmoji() string {
	return normalizeFeishuEmojiType(f.Config.ReactEmoji)
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func normalizeFeishuEmojiType(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "Get"
	}
	key := normalizeEmojiKey(raw)
	if key == "" {
		return "Get"
	}
	if val, ok := feishuEmojiTypeAliases[key]; ok {
		return val
	}
	return raw
}

func normalizeEmojiKey(input string) string {
	if input == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range input {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

var feishuEmojiTypeAliases = func() map[string]string {
	types := []string{
		"OK", "THUMBSUP", "THANKS", "MUSCLE", "FINGERHEART", "APPLAUSE",
		"FISTBUMP", "JIAYI", "DONE", "SMILE", "BLUSH", "LAUGH",
		"SMIRK", "LOL", "FACEPALM", "LOVE", "WINK", "PROUD",
		"WITTY", "SMART", "SCOWL", "THINKING", "SOB", "CRY",
		"ERROR", "NOSEPICK", "HAUGHTY", "SLAP", "SPITBLOOD", "TOASTED",
		"BLACKFACE", "GLANCE", "DULL", "ROSE", "HEART", "PARTY",
		"INNOCENTSMILE", "SHY", "CHUCKLE", "JOYFUL", "WOW", "TRICK",
		"YEAH", "ENOUGH", "TEARS", "EMBARRASSED", "KISS", "SMOOCH",
		"DROOL", "OBSESSED", "MONEY", "TEASE", "SHOWOFF", "COMFORT",
		"CLAP", "PRAISE", "STRIVE", "XBLUSH", "SILENT", "WAVE",
		"EATING", "WHAT", "FROWN", "DULLSTARE", "DIZZY", "LOOKDOWN",
		"WAIL", "CRAZY", "WHIMPER", "HUG", "BLUBBER", "WRONGED",
		"HUSKY", "SHHH", "SMUG", "ANGRY", "HAMMER", "SHOCKED",
		"TERROR", "PETRIFIED", "SKULL", "SWEAT", "SPEECHLESS", "SLEEP",
		"DROWSY", "YAWN", "SICK", "PUKE", "BIGKISS", "BETRAYED",
		"HEADSET", "DONNOTGO", "EatingFood", "Typing", "Lemon", "Get",
		"LGTM", "OnIt", "OneSecond", "Sigh", "MeMeMe", "VRHeadset",
		"YouAreTheBest", "SALUTE", "SHAKE", "HIGHFIVE", "UPPERLEFT", "ThumbsDown",
		"SLIGHT", "TONGUE", "EYESCLOSED", "BEAR", "BULL", "CALF",
		"LIPS", "BEER", "CAKE", "GIFT", "CUCUMBER", "Drumstick",
		"Pepper", "CANDIEDHAWS", "BubbleTea", "Coffee", "Yes", "No",
		"OKR", "CheckMark", "CrossMark", "MinusOne", "Hundred", "AWESOMEN",
		"Pin", "Alarm", "Loudspeaker", "Trophy", "Fire", "RAINBOWPUKE",
		"Music", "XmasTree", "Snowman", "XmasHat", "FIREWORKS", "2022",
		"RoarForYou", "REDPACKET", "FORTUNE", "LUCK", "FIRECRACKER", "StickyRiceBalls",
		"HEARTBROKEN", "BOMB", "POOP", "18X", "CLEAVER", "Soccer",
		"Basketball", "GeneralDoNotDisturb", "Status_PrivateMessage", "GeneralInMeetingBusy", "StatusReading", "StatusFlashOfInspiration",
		"GeneralBusinessTrip", "GeneralWorkFromHome", "StatusEnjoyLife", "GeneralTravellingCar", "StatusBus", "StatusInFlight",
		"GeneralSun", "GeneralMoonRest", "PursueUltimate", "CustomerSuccess", "Responsible", "Reliable",
		"Ambitious", "Patient",
	}
	aliases := make(map[string]string, len(types))
	for _, v := range types {
		aliases[normalizeEmojiKey(v)] = v
	}
	return aliases
}()

func extractSenderID(sender *larkim.EventSender) string {
	if sender == nil || sender.SenderId == nil {
		return ""
	}
	if sender.SenderId.OpenId != nil && *sender.SenderId.OpenId != "" {
		return *sender.SenderId.OpenId
	}
	if sender.SenderId.UserId != nil && *sender.SenderId.UserId != "" {
		return *sender.SenderId.UserId
	}
	if sender.SenderId.UnionId != nil && *sender.SenderId.UnionId != "" {
		return *sender.SenderId.UnionId
	}
	return ""
}

// parseMessageContent extracts text and media from message content.
func (f *FeishuChannel) parseMessageContent(msgType, content string) (string, []string) {
	var contentJSON map[string]interface{}
	if err := json.Unmarshal([]byte(content), &contentJSON); err != nil {
		return content, nil
	}

	switch msgType {
	case "text":
		if text, ok := contentJSON["text"].(string); ok {
			return text, nil
		}
	case "post":
		return f.extractPostContent(contentJSON)
	case "image":
		if key, ok := contentJSON["image_key"].(string); ok {
			return "[image]", []string{key}
		}
	case "file":
		if key, ok := contentJSON["file_key"].(string); ok {
			return "[file]", []string{key}
		}
	case "audio":
		if key, ok := contentJSON["file_key"].(string); ok {
			return "[audio]", []string{key}
		}
	default:
		return fmt.Sprintf("[%s]", msgType), nil
	}

	return "", nil
}

// extractPostContent extracts text from Feishu post (rich text) message.
func (f *FeishuChannel) extractPostContent(contentJSON map[string]interface{}) (string, []string) {
	// Handle wrapped format {"post": {"zh_cn": {...}}}
	if post, ok := contentJSON["post"].(map[string]interface{}); ok {
		contentJSON = post
	}

	// Try localized format
	for _, locale := range []string{"zh_cn", "en_us", "ja_jp"} {
		if localized, ok := contentJSON[locale].(map[string]interface{}); ok {
			return f.parsePostBlock(localized)
		}
	}

	// Try direct format
	return f.parsePostBlock(contentJSON)
}

// parsePostBlock parses a post block.
func (f *FeishuChannel) parsePostBlock(block map[string]interface{}) (string, []string) {
	var texts []string
	var images []string

	if title, ok := block["title"].(string); ok && title != "" {
		texts = append(texts, title)
	}

	if content, ok := block["content"].([]interface{}); ok {
		for _, row := range content {
			if rowSlice, ok := row.([]interface{}); ok {
				for _, el := range rowSlice {
					if elem, ok := el.(map[string]interface{}); ok {
						switch elem["tag"] {
						case "text", "a":
							if text, ok := elem["text"].(string); ok {
								texts = append(texts, text)
							}
						case "at":
							if userName, ok := elem["user_name"].(string); ok {
								texts = append(texts, "@"+userName)
							}
						case "img":
							if key, ok := elem["image_key"].(string); ok {
								images = append(images, key)
							}
						}
					}
				}
			}
		}
	}

	return strings.Join(texts, " "), images
}

// isProcessed checks if a message ID has been processed.
func (f *FeishuChannel) isProcessed(msgID string) bool {
	f.processedMutex.Lock()
	defer f.processedMutex.Unlock()
	_, exists := f.processedMsgIDs[msgID]
	return exists
}

// markProcessed marks a message ID as processed.
func (f *FeishuChannel) markProcessed(msgID string) {
	f.processedMutex.Lock()
	defer f.processedMutex.Unlock()
	f.processedMsgIDs[msgID] = time.Now()
}

// cleanupProcessedMessages periodically cleans up old message IDs.
func (f *FeishuChannel) cleanupProcessedMessages(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f.processedMutex.Lock()
			cutoff := time.Now().Add(-1 * time.Hour)
			for id, t := range f.processedMsgIDs {
				if t.Before(cutoff) {
					delete(f.processedMsgIDs, id)
				}
			}
			f.processedMutex.Unlock()
		}
	}
}

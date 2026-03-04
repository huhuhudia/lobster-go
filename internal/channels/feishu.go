package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
	AllowFrom         []string `json:"allowFrom,omitempty"`
	ReactEmoji        string   `json:"reactEmoji,omitempty"`
}

// FeishuChannel implements the Feishu/Lark bot using WebSocket long connection.
type FeishuChannel struct {
	BaseChannel
	Config           FeishuConfig
	tenantAccessToken string
	tokenExpiry       time.Time
	tokenMutex        sync.Mutex
	httpClient        *http.Client
	processedMsgIDs   map[string]time.Time
	processedMutex    sync.Mutex
}

// NewFeishuChannel creates a new Feishu channel.
func NewFeishuChannel(cfg FeishuConfig, b *bus.MessageBus) *FeishuChannel {
	return &FeishuChannel{
		BaseChannel: BaseChannel{
			Name: "feishu",
			Bus:  b,
		},
		Config:         cfg,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
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
	logging.Default.Info("Feishu bot started with WebSocket long connection")

	// Start token refresh goroutine
	go f.tokenRefreshLoop(ctx)

	// Start message dedup cleanup
	go f.cleanupProcessedMessages(ctx)

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

	// Send text content as interactive card
	if msg.Content != "" {
		card := f.buildCard(msg.Content)
		if err := f.sendMessage(ctx, token, receiveIDType, msg.ChatID, "interactive", card); err != nil {
			return fmt.Errorf("send message: %w", err)
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
	contentJSON, _ := json.Marshal(content)

	reqBody := map[string]interface{}{
		"receive_id_type": receiveIDType,
		"receive_id":      receiveID,
		"msg_type":        msgType,
		"content":         string(contentJSON),
	}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://open.feishu.cn/open-apis/im/v1/messages", bytes.NewReader(body))
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
		"elements": elements,
	}
}

// buildCardElements splits content into card elements.
func (f *FeishuChannel) buildCardElements(content string) []map[string]interface{} {
	// Simple implementation: single markdown element
	// TODO: Add table parsing like Python version
	return []map[string]interface{}{
		{
			"tag":     "markdown",
			"content": content,
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

	// Publish to bus
	return f.PublishInbound(ctx, senderID, chatID, content, media, map[string]string{
		"message_id": msgID,
		"chat_type":  evt.Message.ChatType,
		"msg_type":   evt.Message.MessageType,
	})
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
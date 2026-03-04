package channels

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/huhuhudia/lobster-go/internal/bus"
)

func TestFeishuChannel_IsAllowed(t *testing.T) {
	tests := []struct {
		name      string
		allowFrom []string
		senderID  string
		want      bool
	}{
		{
			name:      "empty list denies all",
			allowFrom: []string{},
			senderID:  "user123",
			want:      false,
		},
		{
			name:      "wildcard allows all",
			allowFrom: []string{"*"},
			senderID:  "user123",
			want:      true,
		},
		{
			name:      "exact match allows",
			allowFrom: []string{"user123", "user456"},
			senderID:  "user123",
			want:      true,
		},
		{
			name:      "no match denies",
			allowFrom: []string{"user123", "user456"},
			senderID:  "user789",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &FeishuChannel{}
			got := f.IsAllowed(tt.allowFrom, tt.senderID)
			if got != tt.want {
				t.Errorf("IsAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFeishuChannel_New(t *testing.T) {
	cfg := FeishuConfig{
		Enabled:   true,
		AppID:     "test_app_id",
		AppSecret: "test_secret",
	}
	b := bus.New(100)

	f := NewFeishuChannel(cfg, b)

	if f.Name != "feishu" {
		t.Errorf("expected name 'feishu', got %s", f.Name)
	}
	if f.Config.AppID != "test_app_id" {
		t.Errorf("expected app_id 'test_app_id', got %s", f.Config.AppID)
	}
}

func TestFeishuChannel_ParseMessageContent(t *testing.T) {
	f := &FeishuChannel{}

	tests := []struct {
		name     string
		msgType  string
		content  string
		wantText string
		wantLen  int
	}{
		{
			name:     "text message",
			msgType:  "text",
			content:  `{"text":"hello world"}`,
			wantText: "hello world",
			wantLen:  0,
		},
		{
			name:     "image message",
			msgType:  "image",
			content:  `{"image_key":"img_123"}`,
			wantText: "[image]",
			wantLen:  1,
		},
		{
			name:     "file message",
			msgType:  "file",
			content:  `{"file_key":"file_123"}`,
			wantText: "[file]",
			wantLen:  1,
		},
		{
			name:     "audio message",
			msgType:  "audio",
			content:  `{"file_key":"audio_123"}`,
			wantText: "[audio]",
			wantLen:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, media := f.parseMessageContent(tt.msgType, tt.content)
			if text != tt.wantText {
				t.Errorf("text = %v, want %v", text, tt.wantText)
			}
			if len(media) != tt.wantLen {
				t.Errorf("media length = %v, want %v", len(media), tt.wantLen)
			}
		})
	}
}

func TestFeishuChannel_ExtractPostContent(t *testing.T) {
	f := &FeishuChannel{}

	tests := []struct {
		name     string
		content  map[string]interface{}
		wantText string
	}{
		{
			name: "simple post",
			content: map[string]interface{}{
				"title": "Test Title",
				"content": []interface{}{
					[]interface{}{
						map[string]interface{}{"tag": "text", "text": "Hello"},
					},
				},
			},
			wantText: "Test Title Hello",
		},
		{
			name: "localized post zh_cn",
			content: map[string]interface{}{
				"zh_cn": map[string]interface{}{
					"title": "中文标题",
					"content": []interface{}{
						[]interface{}{
							map[string]interface{}{"tag": "text", "text": "内容"},
						},
					},
				},
			},
			wantText: "中文标题 内容",
		},
		{
			name: "wrapped post",
			content: map[string]interface{}{
				"post": map[string]interface{}{
					"zh_cn": map[string]interface{}{
						"title": "标题",
						"content": []interface{}{
							[]interface{}{
								map[string]interface{}{"tag": "text", "text": "文本"},
							},
						},
					},
				},
			},
			wantText: "标题 文本",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, _ := f.extractPostContent(tt.content)
			if text != tt.wantText {
				t.Errorf("text = %v, want %v", text, tt.wantText)
			}
		})
	}
}

func TestFeishuChannel_HandleWebhookEvent(t *testing.T) {
	b := bus.New(100)
	f := NewFeishuChannel(FeishuConfig{
		Enabled:   true,
		AppID:     "test",
		AppSecret: "test",
		AllowFrom: []string{"*"},
	}, b)

	// Create a mock message event
	event := map[string]interface{}{
		"schema": "2.0",
		"header": map[string]interface{}{
			"event_id":   "evt_123",
			"event_type": "im.message.receive_v1",
			"token":      "",
		},
		"event": map[string]interface{}{
			"sender": map[string]interface{}{
				"sender_id": map[string]string{
					"open_id": "ou_user123",
				},
				"sender_type": "user",
			},
			"message": map[string]interface{}{
				"message_id":   "msg_123",
				"chat_id":      "oc_chat123",
				"chat_type":    "group",
				"message_type": "text",
				"content":      `{"text":"hello"}`,
			},
		},
	}

	eventJSON, _ := json.Marshal(event)

	err := f.HandleWebhookEvent(context.Background(), eventJSON)
	if err != nil {
		t.Errorf("HandleWebhookEvent failed: %v", err)
	}

	// Should not process same message twice (dedup)
	err = f.HandleWebhookEvent(context.Background(), eventJSON)
	if err != nil {
		t.Errorf("HandleWebhookEvent dedup failed: %v", err)
	}
}

func TestFeishuChannel_Deduplication(t *testing.T) {
	f := &FeishuChannel{
		processedMsgIDs: make(map[string]time.Time),
	}

	msgID := "msg_123"

	// First time should not be processed
	if f.isProcessed(msgID) {
		t.Error("message should not be processed yet")
	}

	// Mark as processed
	f.markProcessed(msgID)

	// Now should be processed
	if !f.isProcessed(msgID) {
		t.Error("message should be marked as processed")
	}
}

func TestFeishuChannel_BuildCard(t *testing.T) {
	f := &FeishuChannel{}

	card := f.buildCard("Hello **world**")

	if card["config"] == nil {
		t.Error("card should have config")
	}

	elements, ok := card["elements"].([]map[string]interface{})
	if !ok {
		t.Fatal("elements should be a slice")
	}

	if len(elements) == 0 {
		t.Fatal("elements should not be empty")
	}

	if elements[0]["tag"] != "markdown" {
		t.Errorf("expected markdown tag, got %v", elements[0]["tag"])
	}
}
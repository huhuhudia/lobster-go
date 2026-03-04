package tools

import (
	"context"
	"errors"

	"github.com/huhuhudia/lobster-go/internal/bus"
	"github.com/huhuhudia/lobster-go/internal/providers"
)

// MessageTool sends a message via bus outbound.
type MessageTool struct {
	Bus *bus.MessageBus
}

func (t MessageTool) Name() string { return "send_message" }

func (t MessageTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: map[string]interface{}{
			"name":        t.Name(),
			"description": "Send a message to a chat",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"channel": map[string]interface{}{
						"type":        "string",
						"description": "Target channel id",
					},
					"chat_id": map[string]interface{}{
						"type":        "string",
						"description": "Chat identifier",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Text content to send",
					},
				},
				"required": []string{"channel", "chat_id", "content"},
			},
		},
	}
}

func (t MessageTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if t.Bus == nil {
		return "", errors.New("bus not set")
	}
	channel, _ := args["channel"].(string)
	chatID, _ := args["chat_id"].(string)
	content, _ := args["content"].(string)
	if channel == "" || chatID == "" || content == "" {
		return "", errors.New("channel, chat_id and content are required")
	}
	msg := bus.OutboundMessage{
		Channel: channel,
		ChatID:  chatID,
		Content: content,
	}
	if err := t.Bus.PublishOutbound(msg); err != nil {
		return "", err
	}
	return "sent", nil
}

package bus

import "time"

// InboundMessage represents a message entering the agent from a channel.
type InboundMessage struct {
	Channel           string            `json:"channel"`
	SenderID          string            `json:"senderId"`
	ChatID            string            `json:"chatId"`
	Content           string            `json:"content"`
	Timestamp         time.Time         `json:"timestamp"`
	Media             []string          `json:"media,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	SessionKeyOverride string           `json:"sessionKeyOverride,omitempty"`
}

// SessionKey returns a stable session key, optionally overridden.
func (m InboundMessage) SessionKey() string {
	if m.SessionKeyOverride != "" {
		return m.SessionKeyOverride
	}
	return m.Channel + ":" + m.ChatID
}

// OutboundMessage represents a message the agent sends to a channel.
type OutboundMessage struct {
	Channel  string            `json:"channel"`
	ChatID   string            `json:"chatId"`
	Content  string            `json:"content"`
	ReplyTo  string            `json:"replyTo,omitempty"`
	Media    []string          `json:"media,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

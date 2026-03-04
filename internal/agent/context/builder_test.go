package context

import (
	"testing"

	"github.com/huhuhudia/lobster-go/internal/session"
)

func TestBuilderAddsSystemAndHistory(t *testing.T) {
	s := session.New("k")
	s.AddMessage("user", "hi")
	b := Builder{SystemPrompt: "you are tester"}
	msgs := b.Build(s, nil)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[1].Role != "user" {
		t.Fatalf("role order mismatch")
	}
}

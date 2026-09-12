package ai

import (
	"context"
	"testing"
)

func TestUnconfiguredClientNeverNeedsNetwork(t *testing.T) {
	c, err := NewUnconfiguredClient(Config{})
	if err != nil {
		t.Fatal(err)
	}
	_, attempt, err := c.call(context.Background(), []byte(`{}`), 1)
	if CodeOf(err) != CodeAIAuthFailed || attempt.ErrorCode != CodeAIAuthFailed {
		t.Fatalf("missing auth: %v", err)
	}
}

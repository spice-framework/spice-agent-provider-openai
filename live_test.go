package openaiprovider

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spice-framework/spice-agent/message"
	"github.com/spice-framework/spice-agent/model"
)

func TestLiveOpenAIResponse(t *testing.T) {
	if os.Getenv("SPICE_OPENAI_LIVE") != "1" {
		t.Skip("set SPICE_OPENAI_LIVE=1 to run the opt-in live acceptance test")
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	modelName := os.Getenv("OPENAI_MODEL")
	if apiKey == "" || modelName == "" {
		t.Fatal("OPENAI_API_KEY and OPENAI_MODEL are required for live acceptance")
	}
	client, err := New(Config{APIKey: apiKey, MaxRetries: 1})
	if err != nil {
		t.Fatal(redactedTestError(err, apiKey))
	}
	part, err := message.Text("Reply with exactly: spice-live-ok")
	if err != nil {
		t.Fatal(err)
	}
	messageID, err := message.NewID("live-message")
	if err != nil {
		t.Fatal(err)
	}
	input, err := message.New(messageID, message.RoleUser, part)
	if err != nil {
		t.Fatal(err)
	}
	request, err := model.NewRequest("live-operation", modelName, []message.Message{input}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	stream, err := client.Stream(ctx, request)
	if err != nil {
		t.Fatal(redactedTestError(err, apiKey))
	}
	defer func() {
		if closeErr := stream.Close(); closeErr != nil {
			t.Errorf("close live stream: %v", redactedTestError(closeErr, apiKey))
		}
	}()
	var text strings.Builder
	completed := false
	for {
		event, receiveErr := stream.Recv(ctx)
		if errors.Is(receiveErr, io.EOF) {
			break
		}
		if receiveErr != nil {
			t.Fatal(redactedTestError(receiveErr, apiKey))
		}
		if delta, ok := event.Text(); ok {
			text.WriteString(delta)
		}
		if event.Kind() == model.EventCompleted {
			completed = true
		}
	}
	if !completed || !strings.Contains(strings.ToLower(text.String()), "spice-live-ok") {
		t.Fatal("live response did not complete with the expected marker")
	}
}

func redactedTestError(err error, secret string) error {
	if err == nil || secret == "" || !strings.Contains(err.Error(), secret) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), secret, "<redacted>"))
}

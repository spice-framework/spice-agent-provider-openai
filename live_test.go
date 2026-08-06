//go:build openai_live

package openaiprovider

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spice-framework/spice-agent/message"
	"github.com/spice-framework/spice-agent/model"
)

func TestLiveOpenAIResponse(t *testing.T) {
	settings, enabled, err := liveAcceptanceSettingsFrom(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Skip("set SPICE_OPENAI_LIVE=1 to run the opt-in live acceptance test")
	}
	client, err := New(Config{
		APIKey: settings.apiKey, Timeout: liveAcceptanceTimeout, MaxRetries: 0,
	})
	if err != nil {
		t.Fatalf("construct live OpenAI client: %v", redactedLiveError(err))
	}
	part, err := message.Text(liveAcceptancePrompt)
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
	request, err := model.NewRequest("live-operation", settings.model, []message.Message{input}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), liveAcceptanceTimeout)
	defer cancel()
	stream, err := client.Stream(ctx, request)
	if err != nil {
		t.Fatalf("start live OpenAI stream: %v", redactedLiveError(err))
	}
	defer func() {
		if closeErr := stream.Close(); closeErr != nil {
			t.Errorf("close live OpenAI stream: %v", redactedLiveError(closeErr))
		}
	}()
	var text strings.Builder
	completed := false
	events := 0
	for {
		event, receiveErr := stream.Recv(ctx)
		if errors.Is(receiveErr, io.EOF) {
			break
		}
		if receiveErr != nil {
			t.Fatalf("receive live OpenAI stream: %v", redactedLiveError(receiveErr))
		}
		if completed {
			t.Fatal("live OpenAI stream emitted an event after terminal completion")
		}
		events++
		if events > maximumLiveAcceptanceEvents {
			t.Fatalf("live OpenAI stream exceeded %d events", maximumLiveAcceptanceEvents)
		}
		if validationErr := event.Validate(); validationErr != nil {
			t.Fatalf("live OpenAI stream returned an invalid event: %v", validationErr)
		}
		if delta, ok := event.Text(); ok {
			if text.Len()+len(delta) > maximumLiveAcceptanceTextBytes {
				t.Fatalf("live OpenAI text exceeded %d bytes", maximumLiveAcceptanceTextBytes)
			}
			text.WriteString(delta)
		}
		if event.Kind() == model.EventCompleted {
			if _, ok := event.Usage(); !ok {
				t.Fatal("live OpenAI completion omitted usage")
			}
			completed = true
		}
	}
	if !completed {
		t.Fatal("live OpenAI stream ended without terminal completion")
	}
	if strings.TrimSpace(text.String()) != liveAcceptanceMarker {
		t.Fatal("live OpenAI response text did not exactly match the expected marker")
	}
}

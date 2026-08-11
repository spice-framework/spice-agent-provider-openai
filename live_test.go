//go:build openai_live

package openaiprovider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/spice-framework/spice-agent/message"
	"github.com/spice-framework/spice-agent/model"
)

func TestLiveResponsesCompatibleProvider(t *testing.T) {
	settings, enabled, err := liveAcceptanceSettingsFrom(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Skip("set SPICE_RESPONSES_LIVE=1 or SPICE_OPENAI_LIVE=1 to run the opt-in live acceptance test")
	}
	ctx, cancel := context.WithTimeout(t.Context(), liveAcceptanceTimeout)
	defer cancel()
	httpClient := &http.Client{Timeout: liveAcceptanceTimeout}
	if settings.zeroCostOnly {
		if err := preflightOpenRouterFreeRoute(ctx, httpClient, settings.model); err != nil {
			t.Fatalf("preflight exact free OpenRouter route: %v", redactedLiveError(err))
		}
	}
	client, err := New(Config{
		APIKey: settings.apiKey, BaseURL: settings.baseURL, Timeout: liveAcceptanceTimeout, MaxRetries: 0,
	}, WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("construct live provider client: %v", redactedLiveError(err))
	}
	if err := applyLiveRequestBounds(client); err != nil {
		t.Fatalf("bound live provider request: %v", redactedLiveError(err))
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
	stream, err := client.Stream(ctx, request)
	if err != nil {
		t.Fatalf("start live provider stream: %v", redactedLiveError(err))
	}
	defer func() {
		if closeErr := stream.Close(); closeErr != nil {
			t.Errorf("close live provider stream: %v", redactedLiveError(closeErr))
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
			t.Fatalf("receive live provider stream: %v", redactedLiveError(receiveErr))
		}
		if completed {
			t.Fatal("live provider stream emitted an event after terminal completion")
		}
		events++
		if events > maximumLiveAcceptanceEvents {
			t.Fatalf("live provider stream exceeded %d events", maximumLiveAcceptanceEvents)
		}
		if validationErr := event.Validate(); validationErr != nil {
			t.Fatalf("live provider stream returned an invalid event: %v", validationErr)
		}
		if delta, ok := event.Text(); ok {
			if text.Len()+len(delta) > maximumLiveAcceptanceTextBytes {
				t.Fatalf("live provider text exceeded %d bytes", maximumLiveAcceptanceTextBytes)
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
		t.Fatal("live provider stream ended without terminal completion")
	}
	if strings.TrimSpace(text.String()) != liveAcceptanceMarker {
		t.Fatal("live provider response text did not exactly match the expected marker")
	}
	fmt.Printf(
		"live-acceptance endpoint_host_class=%s model=%s inference_request_count=1 maximum_elapsed_milliseconds=%d result_sha256=%s catalog_cost_zero=%t\n",
		settings.hostClass,
		settings.model,
		liveAcceptanceTimeout.Milliseconds(),
		liveAcceptanceResultDigest(),
		settings.zeroCostOnly,
	)
}

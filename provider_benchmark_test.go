package openaiprovider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/openai/openai-go/v3/responses"
	"github.com/spice-framework/spice-agent/message"
	"github.com/spice-framework/spice-agent/model"
	"github.com/spice-framework/spice-agent/tool"
)

var (
	benchmarkRequestParams  responses.ResponseNewParams
	benchmarkAllowedTools   map[string]struct{}
	benchmarkStreamEvent    model.StreamEvent
	benchmarkStreamTerminal bool
)

func BenchmarkTranslateRequest(b *testing.B) {
	request := benchmarkConversationRequest(b)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		params, allowed, err := translateRequest(request)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkRequestParams = params
		benchmarkAllowedTools = allowed
	}
}

func BenchmarkTranslateCompletedToolCall(b *testing.B) {
	raw := streamEvent(b, `{"type":"response.completed","sequence_number":1,"response":{"id":"resp-benchmark","model":"benchmark-model","status":"completed","service_tier":"default","usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18},"output":[{"type":"function_call","id":"item-call","call_id":"call-benchmark","name":"read","arguments":"{\"path\":\"README.md\"}","status":"completed"}]}}`)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		value := stream{
			allowedTools: map[string]struct{}{"read": {}},
			seenCalls:    make(map[string]struct{}),
		}
		events, terminal, err := value.translate(raw)
		if err != nil {
			b.Fatal(err)
		}
		if len(events) != 2 || !terminal {
			b.Fatalf("translated events = %d, terminal = %t", len(events), terminal)
		}
		benchmarkStreamEvent = events[0]
		benchmarkStreamTerminal = terminal
	}
}

func BenchmarkScriptedStreamTextAndToolCall(b *testing.B) {
	request := benchmarkRequest(b, true)
	created := streamEvent(b, `{"type":"response.created","sequence_number":1,"response":{"id":"resp-benchmark"}}`)
	delta := streamEvent(b, `{"type":"response.output_text.delta","sequence_number":2,"item_id":"item-message","output_index":0,"content_index":0,"delta":"hello"}`)
	completed := streamEvent(b, `{"type":"response.completed","sequence_number":3,"response":{"id":"resp-benchmark","model":"benchmark-model","status":"completed","service_tier":"default","usage":{"input_tokens":3,"output_tokens":5,"total_tokens":8},"output":[{"type":"message","id":"item-message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello","annotations":[]}]},{"type":"function_call","id":"item-call","call_id":"call-benchmark","name":"read","arguments":"{\"path\":\"README.md\"}","status":"completed"}]}}`)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		source := &scriptedSource{events: []responses.ResponseStreamEventUnion{created, delta, completed}}
		translated, err := testClient(source).Stream(context.Background(), request)
		if err != nil {
			b.Fatal(err)
		}
		count := 0
		for {
			event, receiveErr := translated.Recv(context.Background())
			if errors.Is(receiveErr, io.EOF) {
				break
			}
			if receiveErr != nil {
				b.Fatal(receiveErr)
			}
			benchmarkStreamEvent = event
			count++
		}
		if count != 3 {
			b.Fatalf("stream events = %d", count)
		}
		if err = translated.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRecvCanceled(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	value := &stream{source: &scriptedSource{}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, err := value.Recv(ctx)
		if !errors.Is(err, context.Canceled) {
			b.Fatalf("Recv() error = %v", err)
		}
	}
}

func benchmarkConversationRequest(b *testing.B) model.Request {
	b.Helper()
	definition := benchmarkDefinition(b)
	user := benchmarkMessage(b, "message-user", message.RoleUser, benchmarkText(b, "inspect the repository"))
	assistant := benchmarkMessage(
		b,
		"message-assistant",
		message.RoleAssistant,
		benchmarkText(b, "reading"),
		benchmarkToolCall(b),
	)
	toolResult, err := message.ToolResult(
		"call-history",
		"read",
		json.RawMessage(`{"content":"package openaiprovider"}`),
	)
	if err != nil {
		b.Fatal(err)
	}
	result := benchmarkMessage(b, "message-tool", message.RoleTool, toolResult)
	request, err := model.NewRequest(
		"operation-benchmark",
		"benchmark-model",
		[]message.Message{user, assistant, result},
		[]tool.Definition{definition},
	)
	if err != nil {
		b.Fatal(err)
	}
	return request
}

func benchmarkRequest(b *testing.B, includeTool bool) model.Request {
	b.Helper()
	definitions := []tool.Definition(nil)
	if includeTool {
		definitions = []tool.Definition{benchmarkDefinition(b)}
	}
	request, err := model.NewRequest(
		"operation-benchmark",
		"benchmark-model",
		[]message.Message{benchmarkMessage(b, "message-user", message.RoleUser, benchmarkText(b, "hello"))},
		definitions,
	)
	if err != nil {
		b.Fatal(err)
	}
	return request
}

func benchmarkDefinition(b *testing.B) tool.Definition {
	b.Helper()
	definition, err := tool.NewDefinition(
		"read",
		"Read a file.",
		json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
		tool.EffectReadOnly,
		tool.ReplaySafe,
		tool.CapabilityFilesystemRead,
	)
	if err != nil {
		b.Fatal(err)
	}
	return definition
}

func benchmarkText(b *testing.B, content string) message.Part {
	b.Helper()
	part, err := message.Text(content)
	if err != nil {
		b.Fatal(err)
	}
	return part
}

func benchmarkToolCall(b *testing.B) message.Part {
	b.Helper()
	part, err := message.ToolCall(
		"call-history",
		"read",
		json.RawMessage(`{"path":"provider.go"}`),
	)
	if err != nil {
		b.Fatal(err)
	}
	return part
}

func benchmarkMessage(b *testing.B, id string, role message.Role, parts ...message.Part) message.Message {
	b.Helper()
	messageID, err := message.NewID(id)
	if err != nil {
		b.Fatal(err)
	}
	value, err := message.New(messageID, role, parts...)
	if err != nil {
		b.Fatal(err)
	}
	return value
}

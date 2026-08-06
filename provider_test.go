package openaiprovider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/spice-framework/spice-agent/message"
	"github.com/spice-framework/spice-agent/model"
	"github.com/spice-framework/spice-agent/tool"
)

func TestStreamTranslatesTextFinalizedCallsUsageAndMetadata(t *testing.T) {
	t.Parallel()
	request := testRequest(t, "request-model", true)
	source := &scriptedSource{events: []responses.ResponseStreamEventUnion{
		streamEvent(t, `{"type":"response.created","sequence_number":1,"response":{"id":"resp-1"}}`),
		streamEvent(t, `{"type":"response.output_text.delta","sequence_number":2,"item_id":"item-message","output_index":0,"content_index":0,"delta":"hello"}`),
		streamEvent(t, `{"type":"response.completed","sequence_number":3,"response":{"id":"resp-1","model":"served-model","status":"completed","service_tier":"default","usage":{"input_tokens":3,"output_tokens":5,"total_tokens":8},"output":[{"type":"message","id":"item-message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello","annotations":[]}]},{"type":"function_call","id":"item-call","call_id":"call-actual","name":"read","arguments":"{\"path\":\"README.md\"}","status":"completed"}]}}`),
	}}
	client := &Client{start: func(context.Context, responses.ResponseNewParams, ...option.RequestOption) responseSource {
		return source
	}}

	stream, err := client.Stream(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	events := receiveAll(t, stream)
	if len(events) != 3 {
		t.Fatalf("received %d events, want 3", len(events))
	}
	text, ok := events[0].Text()
	if !ok || text != "hello" {
		t.Fatalf("text event = %q, %v", text, ok)
	}
	call, ok := events[1].Call()
	if !ok || call.ID() != "call-actual" || call.Name() != "read" || string(call.Arguments()) != `{"path":"README.md"}` {
		t.Fatalf("tool event = %#v, %v", call, ok)
	}
	usage, ok := events[2].Usage()
	if !ok || usage.InputTokens() != 3 || usage.OutputTokens() != 5 {
		t.Fatalf("usage = %#v, %v", usage, ok)
	}
	metadata, ok := events[2].Metadata()
	if !ok || len(metadata) != 1 || metadata[0].Namespace() != MetadataNamespace {
		t.Fatalf("metadata = %#v, %v", metadata, ok)
	}
	var facts metadataValue
	if err = json.Unmarshal(metadata[0].Value(), &facts); err != nil {
		t.Fatal(err)
	}
	if facts.ResponseID != "resp-1" || facts.Model != "served-model" || facts.Status != "completed" || facts.ServiceTier != "default" {
		t.Fatalf("metadata facts = %#v", facts)
	}
	if err = stream.Close(); err != nil || !source.closed.Load() {
		t.Fatalf("Close() = %v, closed = %v", err, source.closed.Load())
	}
}

func TestTranslateRequestUsesRequestModelAndPreservesConversation(t *testing.T) {
	t.Parallel()
	request := testConversationRequest(t)
	params, allowed, err := translateRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if params.Model != "authoritative-model" {
		t.Fatalf("translated model = %q", params.Model)
	}
	if _, ok := allowed["read"]; !ok || len(params.Tools) != 1 {
		t.Fatalf("translated tools = %#v, allowed = %#v", params.Tools, allowed)
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, expected := range []string{`"model":"authoritative-model"`, `"store":false`, `"type":"function_call"`, `"call_id":"call-history"`, `"type":"function_call_output"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("translated request does not contain %s: %s", expected, text)
		}
	}
	if strings.Contains(operationKey(request.OperationID()), string(request.OperationID())) {
		t.Fatal("idempotency key exposes operation ID")
	}
}

func TestTranslateRequestRejectsUnsupportedOrMalformedValues(t *testing.T) {
	t.Parallel()
	extension, err := message.Extension("example.com/private", json.RawMessage(`{"value":true}`))
	if err != nil {
		t.Fatal(err)
	}
	withExtension := requestWithParts(t, "model", message.RoleUser, extension)
	if _, _, err = translateRequest(withExtension); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("translateRequest(extension) error = %v", err)
	}
	definition, err := tool.NewDefinition("bad", "Bad schema.", json.RawMessage(`true`))
	if err != nil {
		t.Fatal(err)
	}
	textPart := mustTextPart(t, "hello")
	value := requestWithDefinitions(t, "model", []tool.Definition{definition}, textPart)
	if _, _, err = translateRequest(value); err == nil || !strings.Contains(err.Error(), "JSON object") {
		t.Fatalf("translateRequest(schema) error = %v", err)
	}
}

func TestCompletedRejectsUndeclaredOrProviderNativeTools(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "undeclared function",
			body: `{"type":"response.completed","sequence_number":1,"response":{"id":"r","model":"m","status":"completed","usage":{"input_tokens":1,"output_tokens":1},"output":[{"type":"function_call","id":"i","call_id":"c","name":"write","arguments":"{}","status":"completed"}]}}`,
			want: "protocol_error",
		},
		{
			name: "hosted tool",
			body: `{"type":"response.completed","sequence_number":1,"response":{"id":"r","model":"m","status":"completed","usage":{"input_tokens":1,"output_tokens":1},"output":[{"type":"web_search_call","id":"i","status":"completed"}]}}`,
			want: "protocol_error",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			source := &scriptedSource{events: []responses.ResponseStreamEventUnion{streamEvent(t, test.body)}}
			client := testClient(source)
			stream, err := client.Stream(t.Context(), testRequest(t, "model", true))
			if err != nil {
				t.Fatal(err)
			}
			_, err = stream.Recv(t.Context())
			var streamFailure *model.StreamError
			if err == nil || !errors.As(err, &streamFailure) || streamFailure.Problem().Code() != test.want {
				t.Fatalf("Recv() error = %v, want containing %q", err, test.want)
			}
			mustClose(t, stream)
		})
	}
}

func TestStartErrorIsTypedBoundedAndRedacted(t *testing.T) {
	t.Parallel()
	secret := "do-not-leak"
	response := &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header)}
	response.Header.Set("x-request-id", "request-123")
	source := &scriptedSource{err: &responses.Error{
		Code:       "rate_limit_exceeded",
		Message:    secret,
		StatusCode: http.StatusTooManyRequests,
		Response:   response,
		Request:    httptest.NewRequest(http.MethodPost, "https://example.test/responses", strings.NewReader(secret)),
	}}
	client := testClient(source)
	_, err := client.Stream(t.Context(), testRequest(t, "model", false))
	if err == nil {
		t.Fatal("Stream() error = nil")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Stream() leaked secret: %v", err)
	}
	var providerFailure *model.ProviderError
	if !errors.As(err, &providerFailure) || !providerFailure.Problem().Retryable() {
		t.Fatalf("Stream() error = %T %v", err, err)
	}
	metadata := providerFailure.Problem().Metadata()
	if len(metadata) != 1 || !strings.Contains(string(metadata[0].Value()), "request-123") {
		t.Fatalf("failure metadata = %#v", metadata)
	}
	var typed *Error
	if !errors.As(err, &typed) || typed.StatusCode() != http.StatusTooManyRequests || typed.RequestID() != "request-123" || !typed.Retryable() {
		t.Fatalf("typed failure = %#v", typed)
	}
}

func TestRecvHonorsDistinctContextDeadline(t *testing.T) {
	t.Parallel()
	source := newBlockingSource(streamEvent(t, `{"type":"response.created","sequence_number":1,"response":{"id":"r"}}`))
	client := testClient(source)
	stream, err := client.Stream(t.Context(), testRequest(t, "model", false))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = stream.Recv(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatalf("Recv(deadline) = %v after %s", err, time.Since(started))
	}
	if closeErr := stream.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
}

func TestRecvMayRaceClose(t *testing.T) {
	t.Parallel()
	source := newBlockingSource(streamEvent(t, `{"type":"response.created","sequence_number":1,"response":{"id":"r"}}`))
	stream, err := testClient(source).Stream(t.Context(), testRequest(t, "model", false))
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan error, 1)
	receiveContext := &observedContext{done: make(chan struct{}), entered: make(chan struct{})}
	go func() {
		_, receiveErr := stream.Recv(receiveContext)
		received <- receiveErr
	}()
	select {
	case <-receiveContext.entered:
	case <-time.After(time.Second):
		t.Fatal("Recv did not start waiting")
	}
	if err = stream.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case receiveErr := <-received:
		var streamFailure *model.StreamError
		if !errors.As(receiveErr, &streamFailure) || streamFailure.Problem().Code() != "cancelled" {
			t.Fatalf("Recv racing Close = %T %v", receiveErr, receiveErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Recv did not return after Close")
	}
}

func TestTerminalFailuresAreTypedAndRedacted(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		code string
	}{
		{
			name: "failed",
			raw:  `{"type":"response.failed","sequence_number":1,"response":{"id":"response-failed","model":"served","status":"failed","service_tier":"default","error":{"code":"server_error","message":"private provider detail"}}}`,
			code: "server_error",
		},
		{
			name: "incomplete",
			raw:  `{"type":"response.incomplete","sequence_number":1,"response":{"id":"response-incomplete","model":"served","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`,
			code: "response_incomplete",
		},
		{
			name: "stream error",
			raw:  `{"type":"error","sequence_number":1,"code":"rate_limit_exceeded","message":"private provider detail","param":"input"}`,
			code: "rate_limit_exceeded",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			stream, err := testClient(&scriptedSource{events: []responses.ResponseStreamEventUnion{streamEvent(t, test.raw)}}).
				Stream(t.Context(), testRequest(t, "model", false))
			if err != nil {
				t.Fatal(err)
			}
			event, err := stream.Recv(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			problem, ok := event.Problem()
			if !ok || problem.Code() != test.code || strings.Contains(problem.Message(), "private") {
				t.Fatalf("problem = %#v, %v", problem, ok)
			}
			if _, err = stream.Recv(t.Context()); !errors.Is(err, io.EOF) {
				t.Fatalf("Recv(after terminal) = %v", err)
			}
			mustClose(t, stream)
		})
	}
}

func TestTextReconciliationAndRefusal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		events     []string
		wantText   string
		wantErr    string
		wantEvents int
	}{
		{
			name:     "completion fallback",
			events:   []string{`{"type":"response.completed","sequence_number":1,"response":{"id":"r","model":"m","status":"completed","usage":{"input_tokens":1,"output_tokens":1},"output":[{"type":"message","id":"i","role":"assistant","status":"completed","content":[{"type":"output_text","text":"fallback","annotations":[]}]}]}}`},
			wantText: "fallback", wantEvents: 2,
		},
		{
			name: "missing suffix",
			events: []string{
				`{"type":"response.output_text.delta","sequence_number":1,"item_id":"i","output_index":0,"content_index":0,"delta":"hel"}`,
				`{"type":"response.completed","sequence_number":2,"response":{"id":"r","model":"m","status":"completed","usage":{"input_tokens":1,"output_tokens":1},"output":[{"type":"message","id":"i","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello","annotations":[]}]}]}}`,
			},
			wantText: "hello", wantEvents: 3,
		},
		{
			name: "refusal",
			events: []string{
				`{"type":"response.refusal.delta","sequence_number":1,"item_id":"i","output_index":0,"content_index":0,"delta":"cannot"}`,
				`{"type":"response.completed","sequence_number":2,"response":{"id":"r","model":"m","status":"completed","usage":{"input_tokens":1,"output_tokens":1},"output":[{"type":"message","id":"i","role":"assistant","status":"completed","content":[{"type":"refusal","refusal":"cannot"}]}]}}`,
			},
			wantText: "cannot", wantEvents: 2,
		},
		{
			name: "mismatch",
			events: []string{
				`{"type":"response.output_text.delta","sequence_number":1,"item_id":"i","output_index":0,"content_index":0,"delta":"one"}`,
				`{"type":"response.completed","sequence_number":2,"response":{"id":"r","model":"m","status":"completed","usage":{"input_tokens":1,"output_tokens":1},"output":[{"type":"message","id":"i","role":"assistant","status":"completed","content":[{"type":"output_text","text":"different","annotations":[]}]}]}}`,
			},
			wantText: "one", wantErr: "protocol_error",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			raw := make([]responses.ResponseStreamEventUnion, len(test.events))
			for index, event := range test.events {
				raw[index] = streamEvent(t, event)
			}
			stream, err := testClient(&scriptedSource{events: raw}).Stream(t.Context(), testRequest(t, "model", false))
			if err != nil {
				t.Fatal(err)
			}
			var output strings.Builder
			count := 0
			for {
				event, receiveErr := stream.Recv(t.Context())
				if receiveErr != nil {
					err = receiveErr
					break
				}
				count++
				if text, ok := event.Text(); ok {
					output.WriteString(text)
				}
			}
			if test.wantErr == "" && !errors.Is(err, io.EOF) {
				t.Fatalf("Recv() error = %v", err)
			}
			var streamFailure *model.StreamError
			if test.wantErr != "" {
				if !errors.As(err, &streamFailure) || streamFailure.Problem().Code() != test.wantErr {
					t.Fatalf("Recv() error = %v, want containing %q", err, test.wantErr)
				}
			}
			if output.String() != test.wantText {
				t.Fatalf("text = %q, want %q", output.String(), test.wantText)
			}
			if test.wantEvents != 0 && count != test.wantEvents {
				t.Fatalf("event count = %d, want %d", count, test.wantEvents)
			}
			mustClose(t, stream)
		})
	}
}

func TestCompletedRejectsDuplicateCallsInvalidArgumentsAndUsage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "duplicate",
			raw:  `{"type":"response.completed","sequence_number":1,"response":{"id":"r","model":"m","status":"completed","usage":{"input_tokens":1,"output_tokens":1},"output":[{"type":"function_call","id":"i1","call_id":"same","name":"read","arguments":"{}","status":"completed"},{"type":"function_call","id":"i2","call_id":"same","name":"read","arguments":"{}","status":"completed"}]}}`,
			want: "protocol_error",
		},
		{
			name: "arguments",
			raw:  `{"type":"response.completed","sequence_number":1,"response":{"id":"r","model":"m","status":"completed","usage":{"input_tokens":1,"output_tokens":1},"output":[{"type":"function_call","id":"i","call_id":"call","name":"read","arguments":"not-json","status":"completed"}]}}`,
			want: "protocol_error",
		},
		{
			name: "usage",
			raw:  `{"type":"response.completed","sequence_number":1,"response":{"id":"r","model":"m","status":"completed","usage":{"input_tokens":-1,"output_tokens":1},"output":[]}}`,
			want: "protocol_error",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			stream, err := testClient(&scriptedSource{events: []responses.ResponseStreamEventUnion{streamEvent(t, test.raw)}}).
				Stream(t.Context(), testRequest(t, "model", true))
			if err != nil {
				t.Fatal(err)
			}
			_, err = stream.Recv(t.Context())
			var streamFailure *model.StreamError
			if err == nil || !errors.As(err, &streamFailure) || streamFailure.Problem().Code() != test.want {
				t.Fatalf("Recv() error = %v, want containing %q", err, test.want)
			}
			mustClose(t, stream)
		})
	}
}

func TestTextChunkingPreservesUTF8AndBoundsTotal(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("a", translatedTextChunkBytes-1) + "€" + "tail"
	events, err := textEvents(text)
	if err != nil || len(events) != 2 {
		t.Fatalf("textEvents() count = %d, error = %v", len(events), err)
	}
	var joined strings.Builder
	for _, event := range events {
		part, ok := event.Text()
		if !ok {
			t.Fatal("textEvents returned non-text event")
		}
		joined.WriteString(part)
	}
	if joined.String() != text {
		t.Fatal("textEvents did not preserve UTF-8 text")
	}
	value := &stream{}
	value.text.Grow(model.MaximumOperationTextBytes)
	value.text.WriteString(strings.Repeat("x", model.MaximumOperationTextBytes))
	if _, _, err = value.translateText("overflow"); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("translateText(overflow) error = %v", err)
	}
}

func TestErrorNormalizationAndNilAccessors(t *testing.T) {
	t.Parallel()
	var nilFailure *Error
	if nilFailure.Code() != "" || nilFailure.StatusCode() != 0 || nilFailure.RequestID() != "" || nilFailure.Retryable() || nilFailure.Unwrap() != nil {
		t.Fatal("nil Error accessors returned non-zero values")
	}
	if nilFailure.Error() != "OpenAI provider failure" {
		t.Fatalf("nil Error text = %q", nilFailure.Error())
	}
	cancelled := normalizeFailure(context.Canceled, true)
	if cancelled.Code() != "cancelled" || !errors.Is(cancelled, context.Canceled) || cancelled.Retryable() {
		t.Fatalf("cancelled failure = %#v", cancelled)
	}
	deadline := normalizeFailure(context.DeadlineExceeded, true)
	if deadline.Code() != "deadline_exceeded" || !errors.Is(deadline, context.DeadlineExceeded) {
		t.Fatalf("deadline failure = %#v", deadline)
	}
	transport := normalizeFailure(errors.New("private transport detail"), true)
	if transport.Code() != "transport_error" || strings.Contains(transport.Error(), "private") {
		t.Fatalf("transport failure = %#v", transport)
	}
	if safeCode("BAD CODE") != "provider_error" || safeCode("") != "provider_error" || safeCode("valid_code") != "valid_code" {
		t.Fatal("safeCode validation is incorrect")
	}
	response := &http.Response{StatusCode: http.StatusInternalServerError, Header: make(http.Header)}
	response.Header.Set("x-request-id", " invalid ")
	apiFailure := normalizeFailure(&responses.Error{StatusCode: http.StatusInternalServerError, Response: response}, true)
	if apiFailure.Code() != "http_500" || apiFailure.RequestID() != "invalid" || !apiFailure.Retryable() {
		t.Fatalf("API failure = %#v", apiFailure)
	}
}

func TestHTTPAdapterIsOfflineTestableAndDoesNotRetryPartialStream(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	var sawAuthorization, sawIdempotency atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		sawAuthorization.Store(request.Header.Get("Authorization") == "Bearer fixture-secret")
		sawIdempotency.Store(strings.HasPrefix(request.Header.Get("Idempotency-Key"), "spice-"))
		var body map[string]any
		if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxTranslatedRequestBytes+1)).Decode(&body); err != nil {
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		if body["model"] != "http-model" {
			http.Error(writer, "wrong model", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		if _, writeErr := io.WriteString(writer, "data: {\"type\":\"response.output_text.delta\",\"sequence_number\":1,\"item_id\":\"i\",\"output_index\":0,\"content_index\":0,\"delta\":\"partial\"}\n\n"); writeErr != nil {
			return
		}
	}))
	defer server.Close()
	client, err := New(Config{APIKey: "fixture-secret", BaseURL: server.URL, MaxRetries: 3}, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Stream(t.Context(), testRequest(t, "http-model", false))
	if err != nil {
		t.Fatal(err)
	}
	event, err := stream.Recv(t.Context())
	if err != nil || event.Kind() != model.EventTextDelta {
		t.Fatalf("Recv(text) = %#v, %v", event, err)
	}
	_, err = stream.Recv(t.Context())
	var streamFailure *model.StreamError
	if !errors.As(err, &streamFailure) || streamFailure.Problem().Code() != "transport_error" || streamFailure.Problem().Retryable() {
		t.Fatalf("Recv(incomplete) = %T %v", err, err)
	}
	mustClose(t, stream)
	if requests.Load() != 1 || !sawAuthorization.Load() || !sawIdempotency.Load() {
		t.Fatalf("requests=%d authorization=%v idempotency=%v", requests.Load(), sawAuthorization.Load(), sawIdempotency.Load())
	}
}

func TestStreamRejectsInvalidConstructionAndRequestState(t *testing.T) {
	t.Parallel()
	request := testRequest(t, "model", false)
	for name, client := range map[string]*Client{
		"nil":          nil,
		"unconfigured": {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := client.Stream(t.Context(), request); err == nil {
				t.Fatal("Stream() error = nil")
			}
		})
	}
	var nilContext context.Context
	if _, err := testClient(&scriptedSource{}).Stream(nilContext, request); err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("Stream(nil context) error = %v", err)
	}
	nilSource := &Client{start: func(context.Context, responses.ResponseNewParams, ...option.RequestOption) responseSource { return nil }}
	if _, err := nilSource.Stream(t.Context(), request); err == nil || !strings.Contains(err.Error(), "not created") {
		t.Fatalf("Stream(nil source) error = %v", err)
	}
	if _, err := testClient(&scriptedSource{}).Stream(t.Context(), model.Request{}); err == nil || !strings.Contains(err.Error(), "translation") {
		t.Fatalf("Stream(zero request) error = %v", err)
	}
}

func TestMessageTranslationEnforcesRolePartPairs(t *testing.T) {
	t.Parallel()
	text := mustTextPart(t, "text")
	call := mustToolCallPart(t, "call", "read", json.RawMessage(`{}`))
	result := mustToolResultPart(t, "call", "read", json.RawMessage(`{"ok":true}`))
	tests := []struct {
		name  string
		role  message.Role
		part  message.Part
		valid bool
		want  string
	}{
		{name: "system text", role: message.RoleSystem, part: text, valid: true},
		{name: "user call", role: message.RoleUser, part: call, want: "assistant role"},
		{name: "assistant result", role: message.RoleAssistant, part: result, want: "tool role"},
		{name: "tool text", role: message.RoleTool, part: text, want: "cannot contain text"},
		{name: "tool result", role: message.RoleTool, part: result, valid: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := requestWithParts(t, "model", test.role, test.part)
			_, _, err := translateRequest(request)
			if test.valid && err != nil {
				t.Fatalf("translateRequest() error = %v", err)
			}
			if !test.valid && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("translateRequest() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestStreamReceiveErrorCloseAndCancellation(t *testing.T) {
	t.Parallel()
	var nilStream *stream
	if _, err := nilStream.Recv(t.Context()); err == nil {
		t.Fatal("nil stream Recv() error = nil")
	}
	if err := nilStream.Close(); err != nil {
		t.Fatalf("nil stream Close() = %v", err)
	}
	source := &scriptedSource{
		events: []responses.ResponseStreamEventUnion{streamEvent(t, `{"type":"response.created","sequence_number":1,"response":{"id":"r"}}`)},
		err:    errors.New("private transport detail"),
	}
	active, err := testClient(source).Stream(t.Context(), testRequest(t, "model", false))
	if err != nil {
		t.Fatal(err)
	}
	var nilReceiveContext context.Context
	if _, err = active.Recv(nilReceiveContext); err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("Recv(nil) error = %v", err)
	}
	if _, err = active.Recv(t.Context()); err == nil || !strings.Contains(err.Error(), "transport_error") || strings.Contains(err.Error(), "private") {
		t.Fatalf("Recv(transport error) = %v", err)
	}
	var streamFailure *model.StreamError
	if !errors.As(err, &streamFailure) || streamFailure.Problem().Retryable() {
		t.Fatalf("Recv(transport error) type = %T, problem = %#v", err, streamFailure)
	}
	if err = active.Close(); err != nil {
		t.Fatal(err)
	}
	if err = active.Close(); err != nil {
		t.Fatalf("second Close() = %v", err)
	}
	if _, err = active.Recv(t.Context()); !errors.Is(err, io.EOF) {
		t.Fatalf("Recv(closed) = %v", err)
	}
	closeSource := &scriptedSource{
		events:   []responses.ResponseStreamEventUnion{streamEvent(t, `{"type":"response.created","sequence_number":1,"response":{"id":"r"}}`)},
		closeErr: errors.New("private close detail"),
	}
	closing, err := testClient(closeSource).Stream(t.Context(), testRequest(t, "model", false))
	if err != nil {
		t.Fatal(err)
	}
	err = closing.Close()
	if !errors.As(err, &streamFailure) || strings.Contains(err.Error(), "private") {
		t.Fatalf("Close(transport error) = %v", err)
	}

	operationContext, cancel := context.WithCancel(t.Context())
	blocking := newBlockingSource(streamEvent(t, `{"type":"response.created","sequence_number":1,"response":{"id":"r"}}`))
	cancellable, err := testClient(blocking).Stream(operationContext, testRequest(t, "model", false))
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err = cancellable.Recv(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Recv(cancelled operation) = %v", err)
	}
	mustClose(t, cancellable)
}

func TestMetadataFilteringAndErrorBoundaries(t *testing.T) {
	t.Parallel()
	if metadata, err := errorMetadata(nil); err != nil || metadata != nil {
		t.Fatalf("errorMetadata(nil) = %#v, %v", metadata, err)
	}
	if metadata, err := newMetadata(metadataValue{}); err != nil || metadata != nil {
		t.Fatalf("newMetadata(zero) = %#v, %v", metadata, err)
	}
	if boundedMetadataFact("bad\nvalue") != "" || boundedMetadataFact(strings.Repeat("x", maxMetadataFactBytes+1)) != "" {
		t.Fatal("boundedMetadataFact accepted unsafe input")
	}
	if responseRequestID(nil) != "" {
		t.Fatal("responseRequestID(nil) was non-empty")
	}
	response := &http.Response{Header: make(http.Header)}
	response.Header.Set("x-request-id", strings.Repeat("x", maxRequestIDBytes+1))
	if responseRequestID(response) != "" {
		t.Fatal("responseRequestID accepted oversized value")
	}
	response.Header.Set("x-request-id", "bad\x7fvalue")
	if responseRequestID(response) != "" {
		t.Fatal("responseRequestID accepted control character")
	}
	if statusCode(0) != "provider_error" || statusCode(http.StatusTeapot) != "http_418" {
		t.Fatal("statusCode mapping is incorrect")
	}
}

type scriptedSource struct {
	mu       sync.Mutex
	events   []responses.ResponseStreamEventUnion
	current  responses.ResponseStreamEventUnion
	err      error
	closeErr error
	closed   atomic.Bool
}

func (source *scriptedSource) Next() bool {
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.closed.Load() || len(source.events) == 0 {
		return false
	}
	source.current = source.events[0]
	source.events = source.events[1:]
	return true
}

func (source *scriptedSource) Current() responses.ResponseStreamEventUnion {
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.current
}

func (source *scriptedSource) Err() error { return source.err }
func (source *scriptedSource) Close() error {
	source.closed.Store(true)
	return source.closeErr
}

type blockingSource struct {
	first   responses.ResponseStreamEventUnion
	used    atomic.Bool
	closed  chan struct{}
	close   sync.Once
	current responses.ResponseStreamEventUnion
}

func newBlockingSource(first responses.ResponseStreamEventUnion) *blockingSource {
	return &blockingSource{first: first, closed: make(chan struct{})}
}

func (source *blockingSource) Next() bool {
	if source.used.CompareAndSwap(false, true) {
		source.current = source.first
		return true
	}
	<-source.closed
	return false
}
func (source *blockingSource) Current() responses.ResponseStreamEventUnion { return source.current }
func (*blockingSource) Err() error                                         { return nil }
func (source *blockingSource) Close() error {
	source.close.Do(func() { close(source.closed) })
	return nil
}

type observedContext struct {
	done    <-chan struct{}
	entered chan struct{}
	once    sync.Once
}

func (*observedContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func (ctx *observedContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.entered) })
	return ctx.done
}

func (*observedContext) Err() error    { return nil }
func (*observedContext) Value(any) any { return nil }

func testClient(source responseSource) *Client {
	return &Client{start: func(context.Context, responses.ResponseNewParams, ...option.RequestOption) responseSource {
		return source
	}}
}

func streamEvent(t *testing.T, value string) responses.ResponseStreamEventUnion {
	t.Helper()
	var event responses.ResponseStreamEventUnion
	if err := json.Unmarshal([]byte(value), &event); err != nil {
		t.Fatal(err)
	}
	return event
}

func receiveAll(t *testing.T, stream model.Stream) []model.StreamEvent {
	t.Helper()
	var events []model.StreamEvent
	for {
		event, err := stream.Recv(t.Context())
		if errors.Is(err, io.EOF) {
			return events
		}
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
}

func testRequest(t *testing.T, modelName string, includeTool bool) model.Request {
	t.Helper()
	part, err := message.Text("hello")
	if err != nil {
		t.Fatal(err)
	}
	var definitions []tool.Definition
	if includeTool {
		definition, definitionErr := tool.NewDefinition("read", "Read a file.", json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`), tool.CapabilityFilesystemRead)
		if definitionErr != nil {
			t.Fatal(definitionErr)
		}
		definitions = []tool.Definition{definition}
	}
	return requestWithDefinitions(t, modelName, definitions, part)
}

func requestWithParts(t *testing.T, modelName string, role message.Role, parts ...message.Part) model.Request {
	t.Helper()
	messageID := mustMessageID(t, "message-1")
	value, err := message.New(messageID, role, parts...)
	if err != nil {
		t.Fatal(err)
	}
	request, err := model.NewRequest("operation-1", modelName, []message.Message{value}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func requestWithDefinitions(t *testing.T, modelName string, definitions []tool.Definition, parts ...message.Part) model.Request {
	t.Helper()
	messageID := mustMessageID(t, "message-1")
	value, err := message.New(messageID, message.RoleUser, parts...)
	if err != nil {
		t.Fatal(err)
	}
	request, err := model.NewRequest("operation-1", modelName, []message.Message{value}, definitions)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func testConversationRequest(t *testing.T) model.Request {
	t.Helper()
	text := mustTextPart(t, "working")
	call := mustToolCallPart(t, "call-history", "read", json.RawMessage(`{"path":"README.md"}`))
	result := mustToolResultPart(t, "call-history", "read", json.RawMessage(`{"content":"ok"}`))
	userText := mustTextPart(t, "start")
	messages := []message.Message{
		mustMessage(t, "m1", message.RoleUser, userText),
		mustMessage(t, "m2", message.RoleAssistant, text, call),
		mustMessage(t, "m3", message.RoleTool, result),
	}
	definition := mustToolDefinition(t, "read", "Read a file.", json.RawMessage(`{"type":"object"}`), tool.CapabilityFilesystemRead)
	request, err := model.NewRequest("operation-private", "authoritative-model", messages, []tool.Definition{definition})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func mustMessage(t *testing.T, id string, role message.Role, parts ...message.Part) message.Message {
	t.Helper()
	messageID := mustMessageID(t, id)
	value, err := message.New(messageID, role, parts...)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustMessageID(t *testing.T, value string) message.ID {
	t.Helper()
	result, err := message.NewID(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustTextPart(t *testing.T, value string) message.Part {
	t.Helper()
	result, err := message.Text(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustToolCallPart(t *testing.T, callID, name string, arguments json.RawMessage) message.Part {
	t.Helper()
	result, err := message.ToolCall(callID, name, arguments)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustToolResultPart(t *testing.T, callID, name string, result json.RawMessage) message.Part {
	t.Helper()
	value, err := message.ToolResult(callID, name, result)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustToolDefinition(
	t *testing.T,
	name, description string,
	schema json.RawMessage,
	capabilities ...tool.Capability,
) tool.Definition {
	t.Helper()
	definition, err := tool.NewDefinition(name, description, schema, capabilities...)
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func mustClose(t *testing.T, stream model.Stream) {
	t.Helper()
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
}

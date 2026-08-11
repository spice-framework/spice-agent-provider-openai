package openaiprovider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
)

const (
	responsesLiveAcceptanceOptIn       = "SPICE_RESPONSES_LIVE"
	openAILiveAcceptanceOptIn          = "SPICE_OPENAI_LIVE"
	liveAcceptanceAPIKeyEnvironment    = "OPENAI_API_KEY"
	liveAcceptanceBaseURLEnvironment   = "OPENAI_BASE_URL"
	liveAcceptanceModelEnvironment     = "OPENAI_MODEL"
	liveAcceptanceMarker               = "spice-live-ok"
	liveAcceptancePrompt               = "Reply with exactly: " + liveAcceptanceMarker
	liveAcceptanceTimeout              = 90 * time.Second
	liveAcceptanceMaximumOutputTokens  = 32
	maximumLiveAcceptanceAPIKeyBytes   = 4096
	maximumLiveAcceptanceBaseURLBytes  = 2048
	maximumLiveAcceptanceModelBytes    = 256
	maximumLiveAcceptanceTextBytes     = 4096
	maximumLiveAcceptanceEvents        = 128
	maximumOpenRouterCatalogBytes      = 2 << 20
	openRouterBaseURL                  = "https://openrouter.ai/api/v1"
	openRouterCatalogURL               = openRouterBaseURL + "/models"
	firstPartyOpenAIBaseURL            = "https://api.openai.com/v1"
	liveAcceptanceHostOpenRouter       = "openrouter"
	liveAcceptanceHostFirstPartyOpenAI = "first-party-openai"
	liveAcceptanceHostCompatible       = "responses-compatible"
)

type liveAcceptanceSettings struct {
	apiKey       string
	baseURL      string
	model        string
	hostClass    string
	zeroCostOnly bool
}

type liveAcceptanceEvidence struct {
	Schema                     string `json:"schema"`
	Status                     string `json:"status"`
	EndpointHostClass          string `json:"endpoint_host_class"`
	Model                      string `json:"model"`
	InferenceRequestCount      int    `json:"inference_request_count"`
	MaximumElapsedMilliseconds int64  `json:"maximum_elapsed_milliseconds"`
	ResultSHA256               string `json:"result_sha256"`
	CatalogCostZero            bool   `json:"catalog_cost_zero"`
}

type liveRoundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip liveRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func liveAcceptanceSettingsFrom(getenv func(string) string) (liveAcceptanceSettings, bool, error) {
	if getenv == nil {
		return liveAcceptanceSettings{}, false, errors.New("live provider environment reader is nil")
	}
	responsesEnabled := getenv(responsesLiveAcceptanceOptIn) == "1"
	openAIEnabled := getenv(openAILiveAcceptanceOptIn) == "1"
	if !responsesEnabled && !openAIEnabled {
		return liveAcceptanceSettings{}, false, nil
	}
	if responsesEnabled && openAIEnabled {
		return liveAcceptanceSettings{}, true, errors.New("live acceptance opt-ins are mutually exclusive")
	}
	apiKey := strings.TrimSpace(getenv(liveAcceptanceAPIKeyEnvironment))
	baseURL := strings.TrimSpace(getenv(liveAcceptanceBaseURLEnvironment))
	modelName := strings.TrimSpace(getenv(liveAcceptanceModelEnvironment))
	if err := validateLiveValue(liveAcceptanceAPIKeyEnvironment, "API key", apiKey, maximumLiveAcceptanceAPIKeyBytes); err != nil {
		return liveAcceptanceSettings{}, true, err
	}
	if err := validateLiveValue(liveAcceptanceModelEnvironment, "model", modelName, maximumLiveAcceptanceModelBytes); err != nil {
		return liveAcceptanceSettings{}, true, err
	}
	normalizedURL, hostClass, err := validateLiveBaseURL(baseURL)
	if err != nil {
		return liveAcceptanceSettings{}, true, err
	}
	if openAIEnabled && hostClass != liveAcceptanceHostFirstPartyOpenAI {
		return liveAcceptanceSettings{}, true, errors.New("first-party OpenAI live acceptance requires the exact first-party base URL")
	}
	zeroCostOnly := hostClass == liveAcceptanceHostOpenRouter
	if zeroCostOnly && !strings.HasSuffix(modelName, ":free") {
		return liveAcceptanceSettings{}, true, errors.New("OpenRouter live acceptance requires an exact :free model identifier")
	}
	return liveAcceptanceSettings{
		apiKey:       apiKey,
		baseURL:      normalizedURL,
		model:        modelName,
		hostClass:    hostClass,
		zeroCostOnly: zeroCostOnly,
	}, true, nil
}

func validateLiveValue(environment, name, value string, maximum int) error {
	if value == "" {
		return fmt.Errorf("%s is required for opted-in live acceptance", environment)
	}
	if len(value) > maximum {
		return fmt.Errorf("live provider %s exceeds %d bytes", name, maximum)
	}
	if strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsControl(character) || unicode.IsSpace(character)
	}) >= 0 {
		return fmt.Errorf("live provider %s contains whitespace or control characters", name)
	}
	return nil
}

func validateLiveBaseURL(value string) (string, string, error) {
	if value == "" {
		return "", "", fmt.Errorf("%s is required for opted-in live acceptance", liveAcceptanceBaseURLEnvironment)
	}
	if len(value) > maximumLiveAcceptanceBaseURLBytes {
		return "", "", fmt.Errorf("live provider base URL exceeds %d bytes", maximumLiveAcceptanceBaseURLBytes)
	}
	if strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsControl(character) || unicode.IsSpace(character)
	}) >= 0 {
		return "", "", errors.New("live provider base URL contains whitespace or control characters")
	}
	endpoint, err := url.Parse(value)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return "", "", errors.New("live provider base URL must be an absolute HTTPS URL without user information, query, or fragment")
	}
	normalized := strings.TrimRight(value, "/")
	switch normalized {
	case openRouterBaseURL:
		return normalized, liveAcceptanceHostOpenRouter, nil
	case firstPartyOpenAIBaseURL:
		return normalized, liveAcceptanceHostFirstPartyOpenAI, nil
	default:
		return normalized, liveAcceptanceHostCompatible, nil
	}
}

func applyLiveRequestBounds(client *Client) error {
	if client == nil || client.start == nil {
		return errors.New("live provider client is unavailable")
	}
	start := client.start
	client.start = func(ctx context.Context, params responses.ResponseNewParams, options ...option.RequestOption) responseSource {
		params.MaxOutputTokens = param.NewOpt(int64(liveAcceptanceMaximumOutputTokens))
		return start(ctx, params, options...)
	}
	return nil
}

func preflightOpenRouterFreeRoute(ctx context.Context, client *http.Client, modelName string) error {
	if client == nil {
		return errors.New("OpenRouter catalog preflight dependencies are unavailable")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, openRouterCatalogURL, nil)
	if err != nil {
		return errors.New("construct OpenRouter catalog preflight")
	}
	response, err := client.Do(request)
	if err != nil {
		return errors.New("OpenRouter catalog preflight failed")
	}
	if response.StatusCode != http.StatusOK {
		closeErr := response.Body.Close()
		if closeErr != nil {
			return errors.New("close unsuccessful OpenRouter catalog response")
		}
		return errors.New("OpenRouter catalog preflight returned a non-success status")
	}
	limited := io.LimitReader(response.Body, maximumOpenRouterCatalogBytes+1)
	payload, err := io.ReadAll(limited)
	closeErr := response.Body.Close()
	if err != nil {
		return errors.New("read OpenRouter catalog preflight")
	}
	if closeErr != nil {
		return errors.New("close OpenRouter catalog preflight response")
	}
	if len(payload) > maximumOpenRouterCatalogBytes {
		return fmt.Errorf("OpenRouter catalog exceeds %d bytes", maximumOpenRouterCatalogBytes)
	}
	return validateOpenRouterFreeRoute(payload, modelName)
}

func validateOpenRouterFreeRoute(payload []byte, modelName string) error {
	var catalog struct {
		Data []struct {
			ID      string `json:"id"`
			Pricing struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&catalog); err != nil {
		return errors.New("OpenRouter catalog is not valid JSON")
	}
	if err := requireJSONEnd(decoder); err != nil {
		return errors.New("OpenRouter catalog has trailing JSON content")
	}
	matches := 0
	for _, candidate := range catalog.Data {
		if candidate.ID != modelName {
			continue
		}
		matches++
		if !zeroDecimal(candidate.Pricing.Prompt) || !zeroDecimal(candidate.Pricing.Completion) {
			return errors.New("OpenRouter route does not advertise zero prompt and completion prices")
		}
	}
	if matches != 1 {
		return errors.New("OpenRouter catalog must contain the exact free route once")
	}
	return nil
}

func requireJSONEnd(decoder *json.Decoder) error {
	var trailing json.RawMessage
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return errors.New("JSON input contains trailing content")
}

func zeroDecimal(value string) bool {
	parsed, ok := new(big.Rat).SetString(value)
	return ok && parsed.Sign() == 0
}

func liveAcceptanceResultDigest() string {
	digest := sha256.Sum256([]byte(liveAcceptanceMarker))
	return hex.EncodeToString(digest[:])
}

func redactedLiveError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New("provider details redacted")
}

func TestLiveAcceptanceSettingsRequireExactOptInAndBoundedInputs(t *testing.T) {
	t.Parallel()
	valid := map[string]string{
		responsesLiveAcceptanceOptIn:     "1",
		liveAcceptanceAPIKeyEnvironment:  "fixture-key",
		liveAcceptanceBaseURLEnvironment: openRouterBaseURL,
		liveAcceptanceModelEnvironment:   "fixture/model:free",
	}
	tests := []struct {
		name        string
		environment map[string]string
		enabled     bool
		wantError   string
		wantClass   string
		wantZero    bool
	}{
		{name: "disabled"},
		{name: "non-exact opt in", environment: map[string]string{responsesLiveAcceptanceOptIn: "true"}},
		{name: "responses compatible OpenRouter", environment: valid, enabled: true, wantClass: liveAcceptanceHostOpenRouter, wantZero: true},
		{name: "generic compatible", environment: map[string]string{
			responsesLiveAcceptanceOptIn: "1", liveAcceptanceAPIKeyEnvironment: "fixture-key",
			liveAcceptanceBaseURLEnvironment: "https://responses.example.test/api/v1", liveAcceptanceModelEnvironment: "fixture-model",
		}, enabled: true, wantClass: liveAcceptanceHostCompatible},
		{name: "first party optional mode", environment: map[string]string{
			openAILiveAcceptanceOptIn: "1", liveAcceptanceAPIKeyEnvironment: "fixture-key",
			liveAcceptanceBaseURLEnvironment: firstPartyOpenAIBaseURL, liveAcceptanceModelEnvironment: "fixture-model",
		}, enabled: true, wantClass: liveAcceptanceHostFirstPartyOpenAI},
		{name: "conflicting opt ins", environment: map[string]string{
			responsesLiveAcceptanceOptIn: "1", openAILiveAcceptanceOptIn: "1",
		}, enabled: true, wantError: "mutually exclusive"},
		{name: "missing key", environment: map[string]string{
			responsesLiveAcceptanceOptIn: "1", liveAcceptanceBaseURLEnvironment: openRouterBaseURL,
			liveAcceptanceModelEnvironment: "fixture/model:free",
		}, enabled: true, wantError: liveAcceptanceAPIKeyEnvironment + " is required"},
		{name: "missing base URL", environment: map[string]string{
			responsesLiveAcceptanceOptIn: "1", liveAcceptanceAPIKeyEnvironment: "fixture-key",
			liveAcceptanceModelEnvironment: "fixture/model:free",
		}, enabled: true, wantError: liveAcceptanceBaseURLEnvironment + " is required"},
		{name: "missing model", environment: map[string]string{
			responsesLiveAcceptanceOptIn: "1", liveAcceptanceAPIKeyEnvironment: "fixture-key",
			liveAcceptanceBaseURLEnvironment: openRouterBaseURL,
		}, enabled: true, wantError: liveAcceptanceModelEnvironment + " is required"},
		{name: "key too long", environment: map[string]string{
			responsesLiveAcceptanceOptIn: "1", liveAcceptanceAPIKeyEnvironment: strings.Repeat("k", maximumLiveAcceptanceAPIKeyBytes+1),
			liveAcceptanceBaseURLEnvironment: openRouterBaseURL, liveAcceptanceModelEnvironment: "fixture/model:free",
		}, enabled: true, wantError: "API key exceeds"},
		{name: "model too long", environment: map[string]string{
			responsesLiveAcceptanceOptIn: "1", liveAcceptanceAPIKeyEnvironment: "fixture-key",
			liveAcceptanceBaseURLEnvironment: openRouterBaseURL, liveAcceptanceModelEnvironment: strings.Repeat("m", maximumLiveAcceptanceModelBytes+1),
		}, enabled: true, wantError: "model exceeds"},
		{name: "model whitespace", environment: map[string]string{
			responsesLiveAcceptanceOptIn: "1", liveAcceptanceAPIKeyEnvironment: "fixture-key",
			liveAcceptanceBaseURLEnvironment: openRouterBaseURL, liveAcceptanceModelEnvironment: "bad model:free",
		}, enabled: true, wantError: "whitespace"},
		{name: "OpenRouter model is not free", environment: map[string]string{
			responsesLiveAcceptanceOptIn: "1", liveAcceptanceAPIKeyEnvironment: "fixture-key",
			liveAcceptanceBaseURLEnvironment: openRouterBaseURL, liveAcceptanceModelEnvironment: "fixture/model",
		}, enabled: true, wantError: ":free"},
		{name: "first party opt in rejects compatible host", environment: map[string]string{
			openAILiveAcceptanceOptIn: "1", liveAcceptanceAPIKeyEnvironment: "fixture-key",
			liveAcceptanceBaseURLEnvironment: openRouterBaseURL, liveAcceptanceModelEnvironment: "fixture/model:free",
		}, enabled: true, wantError: "exact first-party"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			settings, enabled, err := liveAcceptanceSettingsFrom(func(name string) string {
				return test.environment[name]
			})
			if enabled != test.enabled {
				t.Fatalf("enabled = %t, want %t", enabled, test.enabled)
			}
			if test.wantError == "" && err != nil {
				t.Fatal(err)
			}
			if test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
				t.Fatalf("error = %v, want containing %q", err, test.wantError)
			}
			secret := test.environment[liveAcceptanceAPIKeyEnvironment]
			if err != nil && secret != "" && strings.Contains(err.Error(), secret) {
				t.Fatal("live acceptance configuration error leaked the API key")
			}
			if err == nil && enabled {
				if settings.apiKey == "" || settings.baseURL == "" || settings.model == "" {
					t.Fatalf("settings = %#v", settings)
				}
				if settings.hostClass != test.wantClass || settings.zeroCostOnly != test.wantZero {
					t.Fatalf("host class/zero = %q/%t, want %q/%t", settings.hostClass, settings.zeroCostOnly, test.wantClass, test.wantZero)
				}
			}
		})
	}
}

func TestLiveAcceptanceBaseURLFailsClosed(t *testing.T) {
	t.Parallel()
	tests := []string{
		"http://openrouter.ai/api/v1",
		"https://user@example.test/v1",
		"https://example.test/v1?route=fallback",
		"https://example.test/v1#fallback",
		"https://example.test/bad path",
		strings.Repeat("h", maximumLiveAcceptanceBaseURLBytes+1),
	}
	for _, value := range tests {
		t.Run(fmt.Sprintf("case-%d", len(value)), func(t *testing.T) {
			t.Parallel()
			if _, _, err := validateLiveBaseURL(value); err == nil {
				t.Fatal("validateLiveBaseURL() succeeded")
			}
		})
	}
}

func TestOpenRouterPreflightUsesOnlyTheFixedBoundedCatalog(t *testing.T) {
	t.Parallel()
	const modelName = "fixture/model:free"
	client := &http.Client{Transport: liveRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.String() != openRouterCatalogURL {
			t.Fatalf("catalog request = %s %s", request.Method, request.URL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(
				`{"data":[{"id":"fixture/model:free","pricing":{"prompt":"0","completion":"0"}}]}`,
			)),
			Header: make(http.Header),
		}, nil
	})}
	if err := preflightOpenRouterFreeRoute(t.Context(), client, modelName); err != nil {
		t.Fatal(err)
	}
	if err := preflightOpenRouterFreeRoute(t.Context(), nil, modelName); err == nil {
		t.Fatal("preflightOpenRouterFreeRoute(..., nil, ...) succeeded")
	}
}

func TestLiveAcceptanceAppliesOutputTokenBound(t *testing.T) {
	t.Parallel()
	var captured responses.ResponseNewParams
	client := &Client{start: func(_ context.Context, params responses.ResponseNewParams, _ ...option.RequestOption) responseSource {
		captured = params
		return &scriptedSource{}
	}}
	if err := applyLiveRequestBounds(client); err != nil {
		t.Fatal(err)
	}
	client.start(t.Context(), responses.ResponseNewParams{})
	if got := captured.MaxOutputTokens.Or(0); got != liveAcceptanceMaximumOutputTokens {
		t.Fatalf("maximum output tokens = %d, want %d", got, liveAcceptanceMaximumOutputTokens)
	}
	if err := applyLiveRequestBounds(nil); err == nil {
		t.Fatal("applyLiveRequestBounds(nil) succeeded")
	}
}

func TestOpenRouterCatalogRequiresOneExactZeroPriceRoute(t *testing.T) {
	t.Parallel()
	const modelName = "fixture/model:free"
	tests := []struct {
		name      string
		payload   string
		wantError string
	}{
		{name: "zero", payload: `{"data":[{"id":"fixture/model:free","pricing":{"prompt":"0","completion":"0.000"}}]}`},
		{name: "missing", payload: `{"data":[]}`, wantError: "exact free route once"},
		{name: "duplicate", payload: `{"data":[{"id":"fixture/model:free","pricing":{"prompt":"0","completion":"0"}},{"id":"fixture/model:free","pricing":{"prompt":"0","completion":"0"}}]}`, wantError: "once"},
		{name: "prompt cost", payload: `{"data":[{"id":"fixture/model:free","pricing":{"prompt":"0.0001","completion":"0"}}]}`, wantError: "zero prompt"},
		{name: "completion cost", payload: `{"data":[{"id":"fixture/model:free","pricing":{"prompt":"0","completion":"0.0001"}}]}`, wantError: "zero prompt"},
		{name: "malformed price", payload: `{"data":[{"id":"fixture/model:free","pricing":{"prompt":"free","completion":"0"}}]}`, wantError: "zero prompt"},
		{name: "malformed JSON", payload: `{`, wantError: "valid JSON"},
		{name: "trailing JSON", payload: `{"data":[]} {}`, wantError: "trailing JSON"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateOpenRouterFreeRoute([]byte(test.payload), modelName)
			if test.wantError == "" && err != nil {
				t.Fatal(err)
			}
			if test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
				t.Fatalf("error = %v, want containing %q", err, test.wantError)
			}
		})
	}
}

func TestLiveAcceptanceEvidenceIsSanitizedAndExact(t *testing.T) {
	t.Parallel()
	payload, err := os.ReadFile("live-acceptance-evidence.json")
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var evidence liveAcceptanceEvidence
	if err := decoder.Decode(&evidence); err != nil {
		t.Fatal(err)
	}
	if err := requireJSONEnd(decoder); err != nil {
		t.Fatal(err)
	}
	want := liveAcceptanceEvidence{
		Schema:                     "spice.agent.provider.live-acceptance/v1alpha1",
		Status:                     "responses-compatible-provider-proven",
		EndpointHostClass:          liveAcceptanceHostOpenRouter,
		Model:                      "poolside/laguna-s-2.1:free",
		InferenceRequestCount:      1,
		MaximumElapsedMilliseconds: liveAcceptanceTimeout.Milliseconds(),
		ResultSHA256:               liveAcceptanceResultDigest(),
		CatalogCostZero:            true,
	}
	if evidence != want {
		t.Fatalf("live acceptance evidence = %#v, want %#v", evidence, want)
	}
	for _, forbidden := range []string{"api_key", "prompt", "output", "token", "response_text", "endpoint_url"} {
		if bytes.Contains(bytes.ToLower(payload), []byte(forbidden)) {
			t.Fatalf("live acceptance evidence contains forbidden field %q", forbidden)
		}
	}
}

func TestLiveAcceptanceResultDigestIsStable(t *testing.T) {
	t.Parallel()
	const expected = "969f379b637f02b0d849fef40b3de43c18fe1d966c15bddc3f91e93bfe94fdc6"
	if got := liveAcceptanceResultDigest(); got != expected {
		t.Fatalf("result digest = %q, want %q", got, expected)
	}
}

func TestLiveAcceptanceErrorsAreUnconditionallyRedacted(t *testing.T) {
	t.Parallel()
	const secret = "live-secret-value"
	err := redactedLiveError(errors.New("upstream exposed " + secret))
	if err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "redacted") {
		t.Fatalf("redactedLiveError() = %v", err)
	}
	if redactedLiveError(nil) != nil {
		t.Fatal("redactedLiveError(nil) is non-nil")
	}
}

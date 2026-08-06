package openaiprovider

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode"
)

const (
	liveAcceptanceOptIn              = "SPICE_OPENAI_LIVE"
	liveAcceptanceMarker             = "spice-live-ok"
	liveAcceptancePrompt             = "Reply with exactly: " + liveAcceptanceMarker
	liveAcceptanceTimeout            = 90 * time.Second
	maximumLiveAcceptanceAPIKeyBytes = 4096
	maximumLiveAcceptanceModelBytes  = 256
	maximumLiveAcceptanceTextBytes   = 4096
	maximumLiveAcceptanceEvents      = 128
)

type liveAcceptanceSettings struct {
	apiKey string
	model  string
}

func liveAcceptanceSettingsFrom(getenv func(string) string) (liveAcceptanceSettings, bool, error) {
	if getenv == nil {
		return liveAcceptanceSettings{}, false, errors.New("live OpenAI environment reader is nil")
	}
	if getenv(liveAcceptanceOptIn) != "1" {
		return liveAcceptanceSettings{}, false, nil
	}
	apiKey := strings.TrimSpace(getenv("OPENAI_API_KEY"))
	modelName := strings.TrimSpace(getenv("OPENAI_MODEL"))
	if err := validateLiveValue("API key", apiKey, maximumLiveAcceptanceAPIKeyBytes); err != nil {
		return liveAcceptanceSettings{}, true, err
	}
	if err := validateLiveValue("model", modelName, maximumLiveAcceptanceModelBytes); err != nil {
		return liveAcceptanceSettings{}, true, err
	}
	return liveAcceptanceSettings{apiKey: apiKey, model: modelName}, true, nil
}

func validateLiveValue(name, value string, maximum int) error {
	if value == "" {
		return fmt.Errorf("OPENAI_%s is required for opted-in live acceptance", strings.ToUpper(strings.ReplaceAll(name, " ", "_")))
	}
	if len(value) > maximum {
		return fmt.Errorf("live OpenAI %s exceeds %d bytes", name, maximum)
	}
	if strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsControl(character) || unicode.IsSpace(character)
	}) >= 0 {
		return fmt.Errorf("live OpenAI %s contains whitespace or control characters", name)
	}
	return nil
}

func redactedLiveError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New("provider details redacted")
}

func TestLiveAcceptanceSettingsRequireExactOptInAndBoundedInputs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		environment map[string]string
		enabled     bool
		wantError   string
	}{
		{name: "disabled"},
		{name: "non-exact opt in", environment: map[string]string{liveAcceptanceOptIn: "true"}},
		{name: "enabled", environment: map[string]string{
			liveAcceptanceOptIn: "1", "OPENAI_API_KEY": "fixture-key", "OPENAI_MODEL": "fixture-model",
		}, enabled: true},
		{name: "missing key", environment: map[string]string{
			liveAcceptanceOptIn: "1", "OPENAI_MODEL": "fixture-model",
		}, enabled: true, wantError: "OPENAI_API_KEY is required"},
		{name: "missing model", environment: map[string]string{
			liveAcceptanceOptIn: "1", "OPENAI_API_KEY": "fixture-key",
		}, enabled: true, wantError: "OPENAI_MODEL is required"},
		{name: "key too long", environment: map[string]string{
			liveAcceptanceOptIn: "1", "OPENAI_API_KEY": strings.Repeat("k", maximumLiveAcceptanceAPIKeyBytes+1), "OPENAI_MODEL": "fixture-model",
		}, enabled: true, wantError: "API key exceeds"},
		{name: "model too long", environment: map[string]string{
			liveAcceptanceOptIn: "1", "OPENAI_API_KEY": "fixture-key", "OPENAI_MODEL": strings.Repeat("m", maximumLiveAcceptanceModelBytes+1),
		}, enabled: true, wantError: "model exceeds"},
		{name: "model whitespace", environment: map[string]string{
			liveAcceptanceOptIn: "1", "OPENAI_API_KEY": "fixture-key", "OPENAI_MODEL": "bad model",
		}, enabled: true, wantError: "whitespace"},
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
			if err != nil && strings.Contains(err.Error(), test.environment["OPENAI_API_KEY"]) && test.environment["OPENAI_API_KEY"] != "" {
				t.Fatal("live acceptance configuration error leaked the API key")
			}
			if err == nil && enabled && (settings.apiKey == "" || settings.model == "") {
				t.Fatalf("settings = %#v", settings)
			}
		})
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

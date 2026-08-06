package openaiprovider

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewConstructsWithoutNetworkAndNormalizesDefaults(t *testing.T) {
	t.Parallel()
	client, err := New(Config{APIKey: " test-secret ", Model: " gpt-test "})
	if err != nil {
		t.Fatal(err)
	}
	if client.Model() != "gpt-test" || client.Responses() == nil {
		t.Fatalf("New() = %#v, want configured Responses client", client)
	}
}

func TestNewRejectsInvalidConfigurationWithoutLeakingSecret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config Config
		want   string
	}{
		{name: "key", config: Config{Model: "model"}, want: "API key is required"},
		{name: "model", config: Config{APIKey: "secret"}, want: "model is required"},
		{name: "scheme", config: Config{APIKey: "secret", Model: "model", BaseURL: "http://api.example.test"}, want: "absolute HTTPS"},
		{name: "userinfo", config: Config{APIKey: "secret", Model: "model", BaseURL: "https://user@api.example.test"}, want: "without user information"},
		{name: "timeout", config: Config{APIKey: "secret", Model: "model", Timeout: -time.Second}, want: "timeout must be positive"},
		{name: "retries low", config: Config{APIKey: "secret", Model: "model", MaxRetries: -1}, want: "between 0 and 8"},
		{name: "retries high", config: Config{APIKey: "secret", Model: "model", MaxRetries: 9}, want: "between 0 and 8"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := New(test.config)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("New() error = %v, want containing %q", err, test.want)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("New() error leaked secret: %v", err)
			}
		})
	}
}

func TestNewValidatesOptions(t *testing.T) {
	t.Parallel()
	config := Config{APIKey: "secret", Model: "model"}
	if _, err := New(config, nil); err == nil || !strings.Contains(err.Error(), "option 0 is nil") {
		t.Fatalf("New(nil option) error = %v", err)
	}
	if _, err := New(config, WithHTTPClient(nil)); err == nil || !strings.Contains(err.Error(), "must not be nil") {
		t.Fatalf("New(nil client) error = %v", err)
	}
	custom := &http.Client{Timeout: time.Second}
	if _, err := New(config, WithHTTPClient(custom)); err != nil {
		t.Fatalf("New(custom HTTP client) error = %v", err)
	}
}

func TestConfigStringRedactsAPIKey(t *testing.T) {
	t.Parallel()
	text := (Config{APIKey: "top-secret", Model: "model"}).String()
	if strings.Contains(text, "top-secret") || !strings.Contains(text, "api_key=<redacted>") {
		t.Fatalf("Config.String() = %q", text)
	}
}

func TestNilClientAccessorsAreSafe(t *testing.T) {
	t.Parallel()
	var client *Client
	if client.Model() != "" || client.Responses() != nil {
		t.Fatal("nil Client accessors returned values")
	}
}

func TestManifest(t *testing.T) {
	t.Parallel()
	spec := Manifest().Spec()
	if spec.Module != "github.com/spice-framework/spice-agent-provider-openai" ||
		len(spec.Dependencies) != 1 ||
		spec.Dependencies[0].Version != "v3.50.0" {
		t.Fatalf("Manifest().Spec() = %#v", spec)
	}
}

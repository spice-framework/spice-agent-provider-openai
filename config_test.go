package openaiprovider

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewConstructsWithoutNetworkAndNormalizesDefaults(t *testing.T) {
	t.Parallel()
	client, err := New(Config{APIKey: " test-secret "})
	if err != nil {
		t.Fatal(err)
	}
	if client.start == nil {
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
		{name: "key", config: Config{}, want: "API key is required"},
		{name: "scheme", config: Config{APIKey: "secret", BaseURL: "http://api.example.test"}, want: "absolute HTTPS"},
		{name: "userinfo", config: Config{APIKey: "secret", BaseURL: "https://user@api.example.test"}, want: "without user information"},
		{name: "timeout negative", config: Config{APIKey: "secret", Timeout: -time.Second}, want: "timeout must be positive"},
		{name: "timeout over maximum", config: Config{APIKey: "secret", Timeout: maximumTimeout + time.Nanosecond}, want: "no greater than 30m0s"},
		{name: "retries low", config: Config{APIKey: "secret", MaxRetries: -1}, want: "between 0 and 8"},
		{name: "retries high", config: Config{APIKey: "secret", MaxRetries: 9}, want: "between 0 and 8"},
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

func TestTimeoutBoundary(t *testing.T) {
	t.Parallel()
	if got := normalizedTimeout(0); got != defaultTimeout {
		t.Fatalf("normalizedTimeout(0) = %s, want %s", got, defaultTimeout)
	}
	if _, err := New(Config{APIKey: "secret", Timeout: maximumTimeout}); err != nil {
		t.Fatalf("New(maximum timeout) error = %v", err)
	}
}

func TestNewValidatesOptions(t *testing.T) {
	t.Parallel()
	config := Config{APIKey: "secret"}
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
	text := (Config{APIKey: "top-secret"}).String()
	if strings.Contains(text, "top-secret") || !strings.Contains(text, "api_key=<redacted>") {
		t.Fatalf("Config.String() = %q", text)
	}
}

func TestManifest(t *testing.T) {
	t.Parallel()
	spec := Manifest().Spec()
	if spec.Module != "github.com/spice-framework/spice-agent-provider-openai" ||
		len(spec.Dependencies) != 2 {
		t.Fatalf("Manifest().Spec() = %#v", spec)
	}
	versions := make(map[string]string, len(spec.Dependencies))
	for _, dependency := range spec.Dependencies {
		versions[dependency.Module] = dependency.Version
	}
	if versions["github.com/spice-framework/spice-agent"] != "v0.0.0-20260806220201-ba45c8884d65" ||
		versions["github.com/openai/openai-go/v3"] != "v3.50.0" {
		t.Fatalf("Manifest().Spec().Dependencies = %#v", spec.Dependencies)
	}
}

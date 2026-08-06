// Package openaiprovider provides the instance-owned OpenAI Responses client
// used by the Spice Agent model adapter.
package openaiprovider

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

const (
	defaultBaseURL = "https://api.openai.com/v1"
	defaultTimeout = 2 * time.Minute
	maximumRetries = 8
)

// Config is the typed, secret-aware configuration for one provider instance.
// APIKey is deliberately excluded from String output and validation errors.
type Config struct {
	APIKey       string        `spice:"api-key,required,secret,env=OPENAI_API_KEY"`
	Model        string        `spice:"model,required,env=OPENAI_MODEL"`
	BaseURL      string        `spice:"base-url,default=https://api.openai.com/v1,env=OPENAI_BASE_URL"`
	Organization string        `spice:"organization,env=OPENAI_ORGANIZATION"`
	Project      string        `spice:"project,env=OPENAI_PROJECT"`
	Timeout      time.Duration `spice:"timeout,default=2m,env=OPENAI_TIMEOUT"`
	MaxRetries   int           `spice:"max-retries,default=0,env=OPENAI_MAX_RETRIES"`
}

// String returns a diagnostic-safe summary.
func (config Config) String() string {
	return fmt.Sprintf(
		"OpenAI(model=%q, base_url=%q, organization=%q, project=%q, timeout=%s, max_retries=%d, api_key=<redacted>)",
		config.Model,
		normalizedBaseURL(config.BaseURL),
		config.Organization,
		config.Project,
		normalizedTimeout(config.Timeout),
		config.MaxRetries,
	)
}

// Client owns one configured OpenAI Responses service. Construction performs
// no network I/O and never consults ambient OpenAI environment variables.
type Client struct {
	responses responses.ResponseService
	model     string
}

// ClientOption customizes client construction without adding hidden globals.
type ClientOption func(*clientOptions) error

type clientOptions struct {
	httpClient *http.Client
}

// WithHTTPClient supplies an instance-owned HTTP client, typically for tests or
// application-specific transport policy.
func WithHTTPClient(client *http.Client) ClientOption {
	return func(options *clientOptions) error {
		if client == nil {
			return errors.New("OpenAI HTTP client must not be nil")
		}
		options.httpClient = client
		return nil
	}
}

// New validates configuration and constructs an instance-owned Responses
// client without making a request.
func New(config Config, options ...ClientOption) (*Client, error) {
	config = normalize(config)
	if err := config.validate(); err != nil {
		return nil, err
	}
	construction := clientOptions{httpClient: &http.Client{Timeout: config.Timeout}}
	for index, apply := range options {
		if apply == nil {
			return nil, fmt.Errorf("OpenAI client option %d is nil", index)
		}
		if err := apply(&construction); err != nil {
			return nil, fmt.Errorf("apply OpenAI client option %d: %w", index, err)
		}
	}
	requestOptions := []option.RequestOption{
		option.WithAPIKey(config.APIKey),
		option.WithBaseURL(config.BaseURL),
		option.WithHTTPClient(construction.httpClient),
		option.WithRequestTimeout(config.Timeout),
		option.WithMaxRetries(config.MaxRetries),
	}
	if config.Organization != "" {
		requestOptions = append(requestOptions, option.WithOrganization(config.Organization))
	}
	if config.Project != "" {
		requestOptions = append(requestOptions, option.WithProject(config.Project))
	}
	return &Client{
		responses: responses.NewResponseService(requestOptions...),
		model:     config.Model,
	}, nil
}

// Model returns the configured model identity.
func (client *Client) Model() string {
	if client == nil {
		return ""
	}
	return client.model
}

// Responses returns the instance-owned SDK service used by the future
// provider-neutral adapter.
func (client *Client) Responses() *responses.ResponseService {
	if client == nil {
		return nil
	}
	return &client.responses
}

func normalize(config Config) Config {
	config.APIKey = strings.TrimSpace(config.APIKey)
	config.Model = strings.TrimSpace(config.Model)
	config.BaseURL = normalizedBaseURL(config.BaseURL)
	config.Organization = strings.TrimSpace(config.Organization)
	config.Project = strings.TrimSpace(config.Project)
	config.Timeout = normalizedTimeout(config.Timeout)
	return config
}

func normalizedBaseURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultBaseURL
	}
	return strings.TrimRight(value, "/")
}

func normalizedTimeout(value time.Duration) time.Duration {
	if value == 0 {
		return defaultTimeout
	}
	return value
}

func (config Config) validate() error {
	if config.APIKey == "" {
		return errors.New("OpenAI API key is required")
	}
	if config.Model == "" {
		return errors.New("OpenAI model is required")
	}
	endpoint, err := url.Parse(config.BaseURL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil {
		return errors.New("OpenAI base URL must be an absolute HTTPS URL without user information")
	}
	if config.Timeout <= 0 {
		return errors.New("OpenAI timeout must be positive")
	}
	if config.MaxRetries < 0 || config.MaxRetries > maximumRetries {
		return fmt.Errorf("OpenAI max retries must be between 0 and %d", maximumRetries)
	}
	return nil
}

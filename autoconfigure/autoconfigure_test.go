package autoconfigure

import (
	"testing"

	provider "github.com/spice-framework/spice-agent-provider-openai"
)

func TestDefaultAndDescriptor(t *testing.T) {
	t.Parallel()
	client, err := Default(provider.Config{APIKey: "fixture"})
	if err != nil || client == nil {
		t.Fatalf("Default() = %#v, %v", client, err)
	}
	descriptor := SpiceAutoConfiguration()
	if descriptor.Review != "docs/dependency-review.md" || len(descriptor.Beans) != 1 {
		t.Fatalf("SpiceAutoConfiguration() = %#v", descriptor)
	}
}

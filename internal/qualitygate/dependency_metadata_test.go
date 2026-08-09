package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModuleGraphIsExactAndRejectsReleaseDrift(t *testing.T) {
	t.Parallel()
	valid := validModuleGraph()
	if err := validateModuleGraph([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		content string
	}{
		{name: "wrong module", content: strings.Replace(valid, modulePath, "example.com/other", 1)},
		{name: "wrong Go", content: strings.Replace(valid, "go "+productGoVersion, "go 1.25.0", 1)},
		{name: "wrong toolchain", content: strings.Replace(valid, productToolchain, "go1.26.4", 1)},
		{name: "stale Spice", content: strings.Replace(valid, spiceVersion, "v0.1.0-preview.1", 1)},
		{name: "stale Agent", content: strings.Replace(valid, spiceAgentVersion, "v0.1.0-preview.3", 1)},
		{name: "wrong OpenAI", content: strings.Replace(valid, openAIGoVersion, "v3.49.0", 1)},
		{name: "wrong Toolchain module", content: strings.Replace(valid, toolchainVersion, "v0.1.0-preview.1", 1)},
		{name: "missing tool", content: strings.Replace(valid, "\ntool "+spiceTool+"\n", "\n", 1)},
		{name: "extra tool", content: valid + "\ntool example.com/other/cmd/tool\n"},
		{name: "replace", content: valid + "\nreplace github.com/spice-framework/spice-agent => ../spice-agent\n"},
		{name: "exclude", content: valid + "\nexclude example.com/other v1.0.0\n"},
		{name: "extra requirement", content: strings.Replace(valid, "require (\n", "require (\n\texample.com/other v1.0.0\n", 1)},
		{name: "directness drift", content: strings.Replace(valid, "golang.org/x/mod v0.38.0", "golang.org/x/mod v0.38.0 // indirect", 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := validateModuleGraph([]byte(test.content)); err == nil {
				t.Fatal("validateModuleGraph() error = nil")
			}
		})
	}
}

func TestCompatibilityMetadataIsExactStrictAndCanonical(t *testing.T) {
	t.Parallel()
	valid := validCompatibilityMetadata()
	if err := validateCompatibilityMetadata([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		content string
	}{
		{name: "unknown field", content: strings.Replace(valid, "\n}\n", ",\n  \"extra\": true\n}\n", 1)},
		{name: "stale Spice", content: strings.Replace(valid, spiceVersion, "v0.1.0-preview.1", 1)},
		{name: "stale Agent", content: strings.Replace(valid, spiceAgentVersion, "v0.1.0-preview.3", 1)},
		{name: "wrong Toolchain", content: strings.Replace(valid, toolchainVersion, "v0.1.0-preview.1", 1)},
		{name: "wrong OpenAI", content: strings.Replace(valid, openAIGoVersion, "v3.49.0", 1)},
		{name: "wrong Go", content: strings.Replace(valid, productToolchain[2:], "1.26.4", 1)},
		{name: "trailing value", content: valid + "{}\n"},
		{name: "noncanonical", content: strings.ReplaceAll(strings.ReplaceAll(valid, "\n", ""), "  ", "")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := validateCompatibilityMetadata([]byte(test.content)); err == nil {
				t.Fatal("validateCompatibilityMetadata() error = nil")
			}
		})
	}
}

func TestDependencyMetadataRequiresBothFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := checkDependencyMetadata(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing go.mod error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(validModuleGraph()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkDependencyMetadata(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing compatibility error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, compatibilityFile), []byte(validCompatibilityMetadata()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkDependencyMetadata(root); err != nil {
		t.Fatal(err)
	}
}

func validModuleGraph() string {
	var builder strings.Builder
	builder.WriteString("module " + modulePath + "\n\ngo " + productGoVersion + "\n\ntoolchain " + productToolchain + "\n\nrequire (\n")
	for _, requirement := range requiredModules() {
		builder.WriteString("\t" + requirement.path + " " + requirement.version)
		if requirement.indirect {
			builder.WriteString(" // indirect")
		}
		builder.WriteByte('\n')
	}
	builder.WriteString(")\n\ntool " + spiceTool + "\n")
	return builder.String()
}

func validCompatibilityMetadata() string {
	return "{\n" +
		"  \"schema\": 1,\n" +
		"  \"minimum\": \"" + spiceVersion + "\",\n" +
		"  \"current\": \"" + spiceVersion + "\",\n" +
		"  \"toolchain\": \"" + toolchainVersion + "\",\n" +
		"  \"spice_agent\": \"" + spiceAgentVersion + "\",\n" +
		"  \"openai_go\": \"" + openAIGoVersion + "\",\n" +
		"  \"go\": \"" + productToolchain[2:] + "\"\n" +
		"}\n"
}

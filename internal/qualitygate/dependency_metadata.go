package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/mod/modfile"
)

const (
	productGoVersion    = "1.26.0"
	productToolchain    = "go1.26.6"
	spiceVersion        = "v0.1.0-preview.4.0.20260814014712-5f535e696300"
	spiceAgentVersion   = "v0.1.0-preview.4"
	toolchainVersion    = "v0.1.0-preview.1.0.20260806203056-d0b9ac086bd6"
	openAIGoVersion     = "v3.50.0"
	spiceTool           = "github.com/spice-framework/toolchain/cmd/spice"
	compatibilityFile   = "spice-compatibility.json"
	compatibilitySchema = 1
)

type requiredModule struct {
	path     string
	version  string
	indirect bool
}

type compatibilityMetadata struct {
	Schema     int    `json:"schema"`
	Minimum    string `json:"minimum"`
	Current    string `json:"current"`
	Toolchain  string `json:"toolchain"`
	SpiceAgent string `json:"spice_agent"`
	OpenAIGo   string `json:"openai_go"`
	Go         string `json:"go"`
}

func requiredModules() []requiredModule {
	return []requiredModule{
		{path: "github.com/openai/openai-go/v3", version: openAIGoVersion},
		{path: "github.com/spice-framework/spice", version: spiceVersion},
		{path: "github.com/spice-framework/spice-agent", version: spiceAgentVersion},
		{path: "github.com/spice-framework/toolchain", version: toolchainVersion, indirect: true},
		{path: "github.com/tidwall/gjson", version: "v1.19.0", indirect: true},
		{path: "github.com/tidwall/match", version: "v1.1.1", indirect: true},
		{path: "github.com/tidwall/pretty", version: "v1.2.1", indirect: true},
		{path: "github.com/tidwall/sjson", version: "v1.2.5", indirect: true},
		{path: "golang.org/x/mod", version: "v0.38.0"},
		{path: "golang.org/x/sync", version: "v0.22.0", indirect: true},
		{path: "golang.org/x/sys", version: "v0.47.0", indirect: true},
		{path: "golang.org/x/tools", version: "v0.48.0", indirect: true},
	}
}

func checkDependencyMetadata(root string) error {
	moduleContent, err := os.ReadFile(filepath.Join(root, "go.mod")) // #nosec G304 -- repository-owned metadata path.
	if err != nil {
		return fmt.Errorf("read go.mod: %w", err)
	}
	if err = validateModuleGraph(moduleContent); err != nil {
		return err
	}
	compatibilityContent, err := os.ReadFile(filepath.Join(root, compatibilityFile)) // #nosec G304 -- repository-owned metadata path.
	if err != nil {
		return fmt.Errorf("read %s: %w", compatibilityFile, err)
	}
	return validateCompatibilityMetadata(compatibilityContent)
}

func validateModuleGraph(content []byte) error {
	file, err := modfile.Parse("go.mod", content, nil)
	if err != nil {
		return fmt.Errorf("parse go.mod: %w", err)
	}
	if err = validateModuleIdentity(file); err != nil {
		return err
	}
	if err = validateModuleDirectives(file); err != nil {
		return err
	}
	return validateModuleRequirements(file.Require)
}

func validateModuleIdentity(file *modfile.File) error {
	if file.Module == nil || file.Module.Mod.Path != modulePath {
		return fmt.Errorf("go.mod module must be %q", modulePath)
	}
	if file.Go == nil || file.Go.Version != productGoVersion {
		return fmt.Errorf("go.mod Go version must be %q", productGoVersion)
	}
	if file.Toolchain == nil || file.Toolchain.Name != productToolchain {
		return fmt.Errorf("go.mod toolchain must be %q", productToolchain)
	}
	return nil
}

func validateModuleDirectives(file *modfile.File) error {
	if len(file.Replace) != 0 {
		return errors.New("go.mod replacements are forbidden in a release candidate")
	}
	if len(file.Exclude) != 0 || len(file.Retract) != 0 || len(file.Godebug) != 0 || len(file.Ignore) != 0 {
		return errors.New("go.mod contains unsupported release directives")
	}
	if len(file.Tool) != 1 || file.Tool[0].Path != spiceTool {
		return fmt.Errorf("go.mod must declare exactly tool %q", spiceTool)
	}
	return nil
}

func validateModuleRequirements(requirements []*modfile.Require) error {
	actual := make(map[string]requiredModule, len(requirements))
	for _, requirement := range requirements {
		if _, duplicate := actual[requirement.Mod.Path]; duplicate {
			return fmt.Errorf("go.mod requires module %q more than once", requirement.Mod.Path)
		}
		actual[requirement.Mod.Path] = requiredModule{
			path:     requirement.Mod.Path,
			version:  requirement.Mod.Version,
			indirect: requirement.Indirect,
		}
	}
	for _, expected := range requiredModules() {
		selected, ok := actual[expected.path]
		if !ok {
			return fmt.Errorf("go.mod must require %s at %s", expected.path, expected.version)
		}
		if selected != expected {
			return fmt.Errorf("go.mod must select %s at %s with indirect=%t", expected.path, expected.version, expected.indirect)
		}
		delete(actual, expected.path)
	}
	if len(actual) != 0 {
		paths := make([]string, 0, len(actual))
		for path := range actual {
			paths = append(paths, path)
		}
		slices.Sort(paths)
		return fmt.Errorf("go.mod contains unauthorized module requirement %q", paths[0])
	}
	return nil
}

func validateCompatibilityMetadata(content []byte) error {
	expected := compatibilityMetadata{
		Schema:     compatibilitySchema,
		Minimum:    spiceVersion,
		Current:    spiceVersion,
		Toolchain:  toolchainVersion,
		SpiceAgent: spiceAgentVersion,
		OpenAIGo:   openAIGoVersion,
		Go:         productToolchain[2:],
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var actual compatibilityMetadata
	if err := decoder.Decode(&actual); err != nil {
		return fmt.Errorf("decode %s: %w", compatibilityFile, err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s has trailing JSON values", compatibilityFile)
	}
	if actual != expected {
		return fmt.Errorf("%s does not match the exact authorized dependency graph", compatibilityFile)
	}
	canonical, err := json.MarshalIndent(expected, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", compatibilityFile, err)
	}
	canonical = append(canonical, '\n')
	if !bytes.Equal(content, canonical) {
		return fmt.Errorf("%s is valid but is not in canonical deterministic form", compatibilityFile)
	}
	return nil
}

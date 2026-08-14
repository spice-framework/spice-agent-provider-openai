//go:build spice_config

package spice

import "github.com/spice-framework/spice/project"

var Build = project.Build{
	Kind: project.StarterKind,
	Dependencies: project.Dependencies{
		project.Library("github.com/openai/openai-go/v3", "v3.50.0"),
		project.Library("github.com/spice-framework/spice-agent", "v0.1.0-preview.4"),
		project.Library("golang.org/x/mod", "v0.38.0"),
		project.BuildTool("github.com/spice-framework/toolchain", "v0.1.0-preview.1.0.20260806203056-d0b9ac086bd6"),
	},
}

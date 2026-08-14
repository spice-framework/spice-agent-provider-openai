//go:build spice_config

package spice

import "github.com/spice-framework/spice/project"

var Settings = project.Settings{
	Name:   "spice-agent-provider-openai",
	Module: "github.com/spice-framework/spice-agent-provider-openai",
	Toolchain: project.Toolchain{
		Go:    "1.26.6",
		Spice: "v0.1.0-preview.4.0.20260814014712-5f535e696300",
	},
	DependencyPolicy: project.DependencyPolicy{
		Verification: project.Strict,
	},
}

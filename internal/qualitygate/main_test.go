package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestNetworkAllowedOnlyForBootstrap(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"fast", "check", "fmt", "benchmark", "verify", "unknown"} {
		if networkAllowed(mode) {
			t.Fatalf("networkAllowed(%q) = true", mode)
		}
	}
	if !networkAllowed("tools-bootstrap") {
		t.Fatal("networkAllowed(tools-bootstrap) = false")
	}
}

func TestBenchmarkArgumentsAreDeterministicAndBounded(t *testing.T) {
	t.Parallel()
	want := []string{
		"test",
		"-run=^$",
		"-bench=^Benchmark(TranslateRequest|TranslateCompletedToolCall|ScriptedStreamTextAndToolCall|RecvCanceled)$",
		"-benchmem",
		"-benchtime=500x",
		"-count=5",
		"-cpu=1",
		".",
	}
	if got := benchmarkArguments(); !slices.Equal(got, want) {
		t.Fatalf("benchmark arguments = %q, want %q", got, want)
	}
}

func TestRepositoryPortabilityRequiresLFAndExplicitToolBootstrap(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeGateFile(t, root, ".gitattributes", "* text=auto eol=lf\n*.pb -text\n*.png -text\n")
	writeGateFile(t, root, ".github/workflows/ci.yml", `steps:
  - run: go run ./internal/qualitygate -mode=tools-bootstrap
  - run: go run ./internal/qualitygate -mode=verify
`)
	if err := checkRepositoryPortability(root); err != nil {
		t.Fatal(err)
	}

	writeGateFile(t, root, ".github/workflows/ci.yml", `steps:
  - run: go run ./internal/qualitygate -mode=verify
`)
	if err := checkRepositoryPortability(root); err == nil || !strings.Contains(err.Error(), "bootstrap") {
		t.Fatalf("missing bootstrap error = %v", err)
	}
}

func TestReleaseWorkflowRequiresExactKeylessBoundary(t *testing.T) {
	t.Parallel()
	const immediatePriorWorkflowCommit = "26de6f4b78a64eedb21e15a2ffe8aa3fd579ef16"
	valid := canonicalReleaseWorkflow()
	for _, test := range []struct {
		name    string
		content string
		omit    bool
		valid   bool
	}{
		{name: "valid", content: valid, valid: true},
		{name: "missing", omit: true},
		{name: "immediate prior authority", content: strings.ReplaceAll(valid, releaseWorkflowCommit, immediatePriorWorkflowCommit)},
		{name: "module drift", content: strings.Replace(valid, modulePath, "example.com/other", 1)},
		{name: "workflow input drift", content: strings.Replace(valid, "workflow_commit: "+releaseWorkflowCommit, "workflow_commit: "+strings.Repeat("0", 40), 1)},
		{name: "workflow-level write", content: strings.Replace(valid, "permissions: {}", "permissions:\n  contents: write", 1)},
		{name: "excess job permission", content: strings.Replace(valid, "      contents: write", "      actions: write\n      contents: write", 1)},
		{name: "missing job permission", content: strings.Replace(valid, "      artifact-metadata: write\n", "", 1)},
		{name: "local step", content: strings.Replace(valid, "    uses:", "    steps:\n      - run: echo unsafe\n    uses:", 1)},
		{name: "inherited secrets", content: valid + "    secrets: inherit\n"},
		{name: "second job", content: valid + "  other:\n    uses: example.com/other.yml@main\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if !test.omit {
				writeGateFile(t, root, ".github/workflows/release.yml", test.content)
			}
			err := checkReleaseWorkflow(root)
			if test.valid && err != nil {
				t.Fatal(err)
			}
			if !test.valid && err == nil {
				t.Fatal("unsafe release workflow passed")
			}
		})
	}
}

func TestExactGoExecutable(t *testing.T) {
	t.Parallel()
	if goExecutableName("windows") != "go.exe" || goExecutableName("linux") != "go" {
		t.Fatal("go executable name is not platform-correct")
	}
	actualName := filepath.Base(exactGoExecutable())
	if (actualName != "go" && actualName != "go.exe") || filepath.Base(filepath.Dir(exactGoExecutable())) != "bin" ||
		qualityExecutable("go") != exactGoExecutable() ||
		qualityExecutable("gofumpt") != "gofumpt" {
		t.Fatalf("exact Go executable = %q", exactGoExecutable())
	}
}

func TestBootstrapDownloadArguments(t *testing.T) {
	t.Parallel()
	moduleFile := filepath.Join("private", "graph.mod")
	want := "mod download -modfile=" + moduleFile + " all"
	if got := strings.Join(bootstrapDownloadArguments(moduleFile), " "); got != want {
		t.Fatalf("bootstrapDownloadArguments() = %q, want %q", got, want)
	}
}

func TestBootstrapPreservesRepositoryOnSuccessAndFailure(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		runnerErr error
	}{
		{name: "success"},
		{name: "failure", runnerErr: errors.New("download failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := bootstrapFixture(t, true)
			before, err := sourceTreeDigests(root)
			if err != nil {
				t.Fatal(err)
			}
			var calls [][]string
			runner := func(_ context.Context, directory string, arguments ...string) error {
				if directory != root && directory != filepath.Join(root, "tools") {
					t.Fatalf("unexpected directory %q", directory)
				}
				calls = append(calls, append([]string(nil), arguments...))
				return test.runnerErr
			}
			err = bootstrapDependencies(context.Background(), root, runner)
			if !errors.Is(err, test.runnerErr) {
				t.Fatalf("bootstrapDependencies() error = %v, want %v", err, test.runnerErr)
			}
			after, digestErr := sourceTreeDigests(root)
			if digestErr != nil {
				t.Fatal(digestErr)
			}
			if len(before) != len(after) {
				t.Fatalf("repository file count changed: %d != %d", len(before), len(after))
			}
			for name, digest := range before {
				if after[name] != digest {
					t.Fatalf("repository file %q changed", name)
				}
			}
			wantCalls := 2
			if test.runnerErr != nil {
				wantCalls = 1
			}
			if len(calls) != wantCalls {
				t.Fatalf("bootstrap calls = %d, want %d", len(calls), wantCalls)
			}
			for _, arguments := range calls {
				if len(arguments) != 4 || arguments[0] != "mod" || arguments[1] != "download" ||
					!strings.HasPrefix(arguments[2], "-modfile=") || arguments[3] != "all" {
					t.Fatalf("unexpected bootstrap arguments: %q", arguments)
				}
				if strings.HasPrefix(strings.TrimPrefix(arguments[2], "-modfile="), root) {
					t.Fatalf("temporary modfile is inside repository: %q", arguments[2])
				}
			}
		})
	}
}

func TestBootstrapAllowsMissingToolsModule(t *testing.T) {
	t.Parallel()
	root := bootstrapFixture(t, false)
	calls := 0
	err := bootstrapDependencies(context.Background(), root, func(_ context.Context, _ string, _ ...string) error {
		calls++
		return nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("bootstrapDependencies() = calls %d, error %v", calls, err)
	}
}

func TestBootstrapPropagatesCancellation(t *testing.T) {
	t.Parallel()
	root := bootstrapFixture(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err := bootstrapDependencies(ctx, root, func(callContext context.Context, _ string, _ ...string) error {
		calls++
		return callContext.Err()
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("bootstrapDependencies() = calls %d, error %v", calls, err)
	}
}

func TestBootstrapDetectsRepositoryMutation(t *testing.T) {
	t.Parallel()
	root := bootstrapFixture(t, false)
	err := bootstrapDependencies(context.Background(), root, func(_ context.Context, directory string, _ ...string) error {
		return os.WriteFile(filepath.Join(directory, "unexpected"), []byte("mutation"), 0o600)
	})
	if err == nil || !strings.Contains(err.Error(), "modified the repository") {
		t.Fatalf("bootstrapDependencies() error = %v", err)
	}
}

func TestCommandEnvironmentSeparatesNetworkAndSecrets(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "must-not-leak")
	t.Setenv("SPICE_TEST_TOKEN", "must-not-leak")
	for _, test := range []struct {
		name    string
		network bool
		proxy   string
	}{
		{name: "offline", proxy: "off"},
		{name: "bootstrap", network: true, proxy: "https://proxy.golang.org"},
	} {
		t.Run(test.name, func(t *testing.T) {
			environment := strings.Join(commandEnvironment(test.network, nil), "\n")
			if strings.Contains(environment, "must-not-leak") || !strings.Contains(environment, "GOPROXY="+test.proxy) {
				t.Fatalf("unsafe command environment:\n%s", environment)
			}
			if test.network && !strings.Contains(environment, "GOAUTH=off") {
				t.Fatalf("bootstrap environment enables Go authentication:\n%s", environment)
			}
		})
	}
}

func bootstrapFixture(t *testing.T, tools bool) string {
	t.Helper()
	root := t.TempDir()
	modules := []string{root}
	if tools {
		modules = append(modules, filepath.Join(root, "tools"))
	}
	for _, directory := range modules {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module example.com/fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "go.sum"), []byte("fixture sum\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func writeGateFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTotalCoverage(t *testing.T) {
	t.Parallel()
	percentage, err := totalCoverage("example.go:1:\tFunction\t100.0%\ntotal:\t(statements)\t87.5%\n")
	if err != nil || percentage != 87.5 {
		t.Fatalf("totalCoverage() = %v, %v", percentage, err)
	}
	for _, content := range []string{"", "total: no-percentage"} {
		if _, err := totalCoverage(content); err == nil {
			t.Fatalf("totalCoverage(%q) error = nil", content)
		}
	}
}

func TestGoFilesExcludesToolsAndVendor(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, name := range []string{"main.go", "nested/value.go", "vendor/ignored.go", "tools/ignored.go"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := goFiles(root)
	if err != nil || len(files) != 2 {
		t.Fatalf("goFiles() = %v, %v", files, err)
	}
	joined := strings.Join(files, " ")
	if strings.Contains(joined, "ignored.go") {
		t.Fatalf("goFiles() included excluded files: %v", files)
	}
}

func TestTreeDigests(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "value"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := treeDigests(root)
	if err != nil {
		t.Fatal(err)
	}
	if writeErr := os.WriteFile(filepath.Join(root, "value"), []byte("two"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	second, err := treeDigests(root)
	if err != nil || first["value"] == second["value"] {
		t.Fatalf("treeDigests() did not detect change: %v", err)
	}
}

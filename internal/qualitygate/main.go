// Command qualitygate runs this repository's cross-platform quality contract.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	modulePath            = "github.com/spice-framework/spice-agent-provider-openai"
	requiredGo            = "go1.26.6"
	minimumCoverage       = 85.0
	releaseWorkflowCommit = "0fcd43dc8b41fad56c231d0e136ad8c762276ed5"
)

func main() {
	os.Exit(execute())
}

func execute() int {
	mode := flag.String("mode", "check", "tools-bootstrap, fast, check, fmt, benchmark, or verify")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	root, err := repositoryRoot()
	if err == nil {
		err = run(ctx, root, *mode)
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "qualitygate:", err)
		return 1
	}
	return 0
}

func run(ctx context.Context, root, mode string) error {
	if runtime.Version() != requiredGo {
		return fmt.Errorf("requires %s, got %s", requiredGo, runtime.Version())
	}
	if networkAllowed(mode) {
		return bootstrapDependencies(ctx, root, networkCommand)
	}
	switch mode {
	case "fast":
		if err := checkRepositoryMetadata(root); err != nil {
			return err
		}
		return command(ctx, root, nil, "go", "test", "-shuffle=on", "-count=1", "./...")
	case "fmt":
		return format(ctx, root, true)
	case "check":
		return check(ctx, root)
	case "benchmark":
		if err := checkRepositoryMetadata(root); err != nil {
			return err
		}
		return benchmarks(ctx, root)
	case "verify":
		if err := check(ctx, root); err != nil {
			return err
		}
		for _, gate := range []func(context.Context, string) error{
			lint, security, raceTests, coverage, vendor, offline,
		} {
			if err := gate(ctx, root); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported mode %q", mode)
	}
}

func networkAllowed(mode string) bool { return mode == "tools-bootstrap" }

func benchmarks(ctx context.Context, root string) error {
	environment := map[string]string{
		"GOFLAGS": "-mod=vendor", "GOPROXY": "off", "GOSUMDB": "off",
		"GOTOOLCHAIN": "local", "GOWORK": "off",
	}
	return command(ctx, root, environment, "go", benchmarkArguments()...)
}

func benchmarkArguments() []string {
	return []string{
		"test",
		"-run=^$",
		"-bench=^Benchmark(TranslateRequest|TranslateCompletedToolCall|ScriptedStreamTextAndToolCall|RecvCanceled)$",
		"-benchmem",
		"-benchtime=500x",
		"-count=5",
		"-cpu=1",
		".",
	}
}

type bootstrapRunner func(context.Context, string, ...string) error

type moduleGraph struct {
	directory string
	optional  bool
}

func bootstrapDependencies(ctx context.Context, root string, runner bootstrapRunner) (returnErr error) {
	before, err := sourceTreeDigests(root)
	if err != nil {
		return fmt.Errorf("snapshot repository before bootstrap: %w", err)
	}
	defer func() {
		after, snapshotErr := sourceTreeDigests(root)
		if snapshotErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("snapshot repository after bootstrap: %w", snapshotErr))
			return
		}
		if !maps.Equal(before, after) {
			returnErr = errors.Join(returnErr, errors.New("dependency bootstrap modified the repository"))
		}
	}()

	graphs := []moduleGraph{{directory: root}, {directory: filepath.Join(root, "tools"), optional: true}}
	for _, graph := range graphs {
		if err := bootstrapModuleGraph(ctx, graph, runner); err != nil {
			return err
		}
	}
	return nil
}

func bootstrapModuleGraph(ctx context.Context, graph moduleGraph, runner bootstrapRunner) (returnErr error) {
	moduleFile := filepath.Join(graph.directory, "go.mod")
	moduleContent, err := os.ReadFile(moduleFile) // #nosec G304 -- repository-owned module graph.
	if errors.Is(err, os.ErrNotExist) && graph.optional {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", moduleFile, err)
	}
	temporary, err := os.MkdirTemp("", "spice-tools-bootstrap-*")
	if err != nil {
		return fmt.Errorf("create dependency bootstrap directory: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, os.RemoveAll(temporary)) }()
	temporaryRoot, err := os.OpenRoot(temporary)
	if err != nil {
		return fmt.Errorf("open dependency bootstrap directory: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, temporaryRoot.Close()) }()

	temporaryModule := filepath.Join(temporary, "graph.mod")
	if writeErr := temporaryRoot.WriteFile("graph.mod", moduleContent, 0o600); writeErr != nil {
		return fmt.Errorf("write temporary module file: %w", writeErr)
	}
	sumFile := filepath.Join(graph.directory, "go.sum")
	sumContent, err := os.ReadFile(sumFile) // #nosec G304 -- repository-owned module graph.
	if err == nil {
		if writeErr := temporaryRoot.WriteFile("graph.sum", sumContent, 0o600); writeErr != nil {
			return fmt.Errorf("write temporary checksum file: %w", writeErr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", sumFile, err)
	}
	return runner(ctx, graph.directory, bootstrapDownloadArguments(temporaryModule)...)
}

func bootstrapDownloadArguments(moduleFile string) []string {
	return []string{"mod", "download", "-modfile=" + moduleFile, "all"}
}

func check(ctx context.Context, root string) error {
	if err := checkRepositoryMetadata(root); err != nil {
		return err
	}
	for _, gate := range []func(context.Context, string) error{
		formatCheck, moduleCheck, vet, tests,
	} {
		if err := gate(ctx, root); err != nil {
			return err
		}
	}
	return nil
}

func checkRepositoryMetadata(root string) error {
	if err := checkReleaseMetadata(root); err != nil {
		return err
	}
	if err := checkDependencyMetadata(root); err != nil {
		return err
	}
	if err := checkRepositoryPortability(root); err != nil {
		return err
	}
	return checkReleaseWorkflow(root)
}

func checkReleaseWorkflow(root string) error {
	content, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml")) // #nosec G304 -- repository-owned path.
	if err != nil {
		return fmt.Errorf("read release workflow: %w", err)
	}
	if strings.ReplaceAll(string(content), "\r\n", "\n") != canonicalReleaseWorkflow() {
		return errors.New("release workflow must be the exact single-job, secret-free, permission-bounded reusable caller")
	}
	return nil
}

func canonicalReleaseWorkflow() string {
	return "name: Release\n\n" +
		"on:\n" +
		"  push:\n" +
		"    tags:\n" +
		"      - \"v[0-9]*.[0-9]*.[0-9]*\"\n\n" +
		"permissions: {}\n\n" +
		"jobs:\n" +
		"  release:\n" +
		"    name: Keylessly attest and publish\n" +
		"    permissions:\n" +
		"      contents: write\n" +
		"      id-token: write\n" +
		"      attestations: write\n" +
		"      artifact-metadata: write\n" +
		"    uses: spice-framework/.github/.github/workflows/go-module-release.yml@" + releaseWorkflowCommit + "\n" +
		"    with:\n" +
		"      module: " + modulePath + "\n" +
		"      workflow_commit: " + releaseWorkflowCommit + "\n"
}

func checkRepositoryPortability(root string) error {
	attributes, err := os.ReadFile(filepath.Join(root, ".gitattributes")) // #nosec G304 -- repository-owned path.
	if err != nil {
		return fmt.Errorf("read .gitattributes: %w", err)
	}
	if string(attributes) != "* text=auto eol=lf\n*.pb -text\n*.png -text\n" {
		return errors.New(".gitattributes must enforce LF text and preserve binary protocol/image files")
	}
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yml")) // #nosec G304 -- repository-owned path.
	if err != nil {
		return fmt.Errorf("read CI workflow: %w", err)
	}
	text := strings.ReplaceAll(string(workflow), "\r\n", "\n")
	bootstrap := strings.Index(text, "go run ./internal/qualitygate -mode=tools-bootstrap")
	verify := strings.Index(text, "go run ./internal/qualitygate -mode=verify")
	if bootstrap < 0 || verify <= bootstrap {
		return errors.New("CI quality jobs must bootstrap pinned tools before offline verification")
	}
	return nil
}

func formatCheck(ctx context.Context, root string) error { return format(ctx, root, false) }

func format(ctx context.Context, root string, write bool) error {
	files, err := goFiles(root)
	if err != nil {
		return err
	}
	option := "-l"
	if write {
		option = "-w"
	}
	for _, name := range []string{"goimports", "gofumpt"} {
		executable, err := toolPath(ctx, root, name)
		if err != nil {
			return err
		}
		stdout, err := capture(ctx, root, executable, append([]string{option}, files...)...)
		if err != nil {
			return err
		}
		if !write && strings.TrimSpace(stdout) != "" {
			return fmt.Errorf("%s requires formatting: %s", name, strings.Join(strings.Fields(stdout), ", "))
		}
	}
	return nil
}

func moduleCheck(ctx context.Context, root string) error {
	return command(ctx, root, nil, "go", "mod", "tidy", "-diff")
}

func vet(ctx context.Context, root string) error {
	return command(ctx, root, nil, "go", "vet", "./...")
}

func tests(ctx context.Context, root string) error {
	return command(ctx, root, nil, "go", "test", "-shuffle=on", "-count=1", "./...")
}

func raceTests(ctx context.Context, root string) error {
	return command(ctx, root, nil, "go", "test", "-race", "-shuffle=on", "-count=1", "./...")
}

func lint(ctx context.Context, root string) error {
	golangci, err := toolPath(ctx, root, "golangci-lint")
	if err != nil {
		return err
	}
	if runErr := command(ctx, root, nil, golangci, "run", "--timeout=10m"); runErr != nil {
		return runErr
	}
	nilaway, err := toolPath(ctx, root, "nilaway")
	if err != nil {
		return err
	}
	return command(ctx, root, nil, nilaway, "-include-pkgs="+modulePath, "./...")
}

func security(ctx context.Context, root string) error {
	gosec, err := toolPath(ctx, root, "gosec")
	if err != nil {
		return err
	}
	if runErr := command(ctx, root, nil, gosec, "-quiet", "-exclude-generated", "./..."); runErr != nil {
		return runErr
	}
	govulncheck, err := toolPath(ctx, root, "govulncheck")
	if err != nil {
		return err
	}
	return command(ctx, root, nil, govulncheck, "./...")
}

func coverage(ctx context.Context, root string) (returnErr error) {
	packages, err := productPackages(ctx, root)
	if err != nil {
		return err
	}
	profile, err := os.CreateTemp("", "spice-agent-openai-coverage-*.out")
	if err != nil {
		return fmt.Errorf("create coverage profile: %w", err)
	}
	path := profile.Name()
	if closeErr := profile.Close(); closeErr != nil {
		return fmt.Errorf("close coverage profile: %w", closeErr)
	}
	defer func() { returnErr = errors.Join(returnErr, os.Remove(path)) }()
	arguments := append([]string{"test", "-covermode=atomic", "-coverprofile=" + path}, packages...)
	if runErr := command(ctx, root, nil, "go", arguments...); runErr != nil {
		return runErr
	}
	report, err := capture(ctx, root, "go", "tool", "cover", "-func="+path)
	if err != nil {
		return err
	}
	percentage, err := totalCoverage(report)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "coverage %.1f%% (minimum %.1f%%)\n", percentage, minimumCoverage); err != nil {
		return fmt.Errorf("write coverage result: %w", err)
	}
	if percentage < minimumCoverage {
		return fmt.Errorf("coverage %.1f%% is below %.1f%%", percentage, minimumCoverage)
	}
	return nil
}

func productPackages(ctx context.Context, root string) ([]string, error) {
	stdout, err := capture(ctx, root, "go", "list", "-f={{.ImportPath}}", "./...")
	if err != nil {
		return nil, err
	}
	qualityPackage := modulePath + "/internal/qualitygate"
	var result []string
	for candidate := range strings.FieldsSeq(stdout) {
		if candidate != qualityPackage {
			result = append(result, candidate)
		}
	}
	slices.Sort(result)
	if len(result) == 0 {
		return nil, errors.New("no product packages found")
	}
	return result, nil
}

func totalCoverage(report string) (float64, error) {
	lines := strings.Split(strings.TrimSpace(report), "\n")
	if len(lines) == 0 {
		return 0, errors.New("coverage report is empty")
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) == 0 || !strings.HasSuffix(fields[len(fields)-1], "%") {
		return 0, errors.New("coverage report has no total percentage")
	}
	return strconv.ParseFloat(strings.TrimSuffix(fields[len(fields)-1], "%"), 64)
}

func vendor(ctx context.Context, root string) (returnErr error) {
	temporary, err := os.MkdirTemp("", "spice-agent-openai-vendor-*")
	if err != nil {
		return fmt.Errorf("create vendor comparison directory: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, os.RemoveAll(temporary)) }()
	candidate := filepath.Join(temporary, "vendor")
	if vendorErr := command(ctx, root, nil, "go", "mod", "vendor", "-o", candidate); vendorErr != nil {
		return vendorErr
	}
	current, err := treeDigests(filepath.Join(root, "vendor"))
	if err != nil {
		return err
	}
	expected, err := treeDigests(candidate)
	if err != nil {
		return err
	}
	if !maps.Equal(current, expected) {
		return errors.New("vendor differs from a fresh go mod vendor result")
	}
	return nil
}

func offline(ctx context.Context, root string) error {
	environment := map[string]string{"GOFLAGS": "-mod=vendor"}
	if err := command(ctx, root, environment, "go", "test", "-count=1", "./..."); err != nil {
		return err
	}
	return command(ctx, root, environment, "go", "build", "-trimpath", "./...")
}

func goFiles(root string) ([]string, error) {
	var result []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && path != root && slices.Contains([]string{".git", "tools", "vendor"}, entry.Name()) {
			return filepath.SkipDir
		}
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".go" {
			result = append(result, path)
		}
		return nil
	})
	slices.Sort(result)
	return result, err
}

func treeDigests(root string) (_ map[string][sha256.Size]byte, returnErr error) {
	return digests(root, false)
}

func sourceTreeDigests(root string) (_ map[string][sha256.Size]byte, returnErr error) {
	return digests(root, true)
}

func digests(root string, excludeGit bool) (_ map[string][sha256.Size]byte, returnErr error) {
	opened, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open tree %q: %w", root, err)
	}
	defer func() { returnErr = errors.Join(returnErr, opened.Close()) }()
	result := make(map[string][sha256.Size]byte)
	err = fs.WalkDir(opened.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if excludeGit && path == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		content, readErr := opened.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		result[filepath.ToSlash(path)] = sha256.Sum256(content)
		return nil
	})
	return result, err
}

func toolPath(ctx context.Context, root, name string) (string, error) {
	stdout, err := capture(ctx, root, "go", "tool", "-C", "tools", "-n", name)
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(stdout)
	if path == "" {
		return "", fmt.Errorf("resolve tool %q: empty path", name)
	}
	return path, nil
}

func repositoryRoot() (string, error) {
	current, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	for {
		content, readErr := os.ReadFile(filepath.Join(current, "go.mod")) // #nosec G304 -- candidates are bounded ancestors.
		if readErr == nil && bytes.Contains(content, []byte("module "+modulePath)) {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("repository go.mod not found")
		}
		current = parent
	}
}

func command(ctx context.Context, directory string, environment map[string]string, executable string, arguments ...string) error {
	executable = qualityExecutable(executable)
	// #nosec G204,G702 -- executable and arguments are repository-owned gate values.
	cmd := exec.CommandContext(ctx, executable, arguments...)
	cmd.Dir = directory
	cmd.Env = mergedEnvironment(environment)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", executable, strings.Join(arguments, " "), err)
	}
	return nil
}

func networkCommand(ctx context.Context, directory string, arguments ...string) error {
	// #nosec G204,G702 -- only the exact copied module graphs are downloaded.
	cmd := exec.CommandContext(ctx, exactGoExecutable(), arguments...)
	cmd.Dir = directory
	cmd.Env = commandEnvironment(true, nil)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go %s: %w", strings.Join(arguments, " "), err)
	}
	return nil
}

func capture(ctx context.Context, directory, executable string, arguments ...string) (string, error) {
	executable = qualityExecutable(executable)
	// #nosec G204,G702 -- executable and arguments are repository-owned gate values.
	cmd := exec.CommandContext(ctx, executable, arguments...)
	cmd.Dir = directory
	cmd.Env = mergedEnvironment(nil)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w\n%s", executable, strings.Join(arguments, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func qualityExecutable(executable string) string {
	if executable == "go" {
		return exactGoExecutable()
	}
	return executable
}

func exactGoExecutable() string {
	return filepath.Join(runtime.GOROOT(), "bin", goExecutableName(runtime.GOOS)) //nolint:staticcheck // Gate runs in place under the selected exact toolchain.
}

func goExecutableName(goos string) string {
	if goos == "windows" {
		return "go.exe"
	}
	return "go"
}

func mergedEnvironment(overrides map[string]string) []string {
	return commandEnvironment(false, overrides)
}

func commandEnvironment(network bool, overrides map[string]string) []string {
	values := map[string]string{"GOWORK": "off", "GOFLAGS": "", "GOTOOLCHAIN": "local"}
	if network {
		values["GOAUTH"] = "off"
		values["GONOPROXY"] = ""
		values["GONOSUMDB"] = ""
		values["GOPRIVATE"] = ""
		values["GOPROXY"] = "https://proxy.golang.org"
		values["GOSUMDB"] = "sum.golang.org"
	} else {
		values["GOPROXY"] = "off"
	}
	maps.Copy(values, overrides)
	result := make([]string, 0, len(os.Environ())+len(values))
	for _, entry := range os.Environ() {
		key, _, found := strings.Cut(entry, "=")
		if found {
			upperKey := strings.ToUpper(key)
			if sensitiveEnvironmentKey(upperKey) {
				continue
			}
			if _, replaced := values[upperKey]; !replaced {
				result = append(result, entry)
			}
		}
	}
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	slices.Sort(result)
	return result
}

func sensitiveEnvironmentKey(key string) bool {
	return strings.Contains(key, "TOKEN") || strings.Contains(key, "PASSWORD") ||
		strings.Contains(key, "SECRET") || strings.HasSuffix(key, "API_KEY") ||
		strings.HasSuffix(key, "ACCESS_KEY") || strings.HasSuffix(key, "PRIVATE_KEY")
}

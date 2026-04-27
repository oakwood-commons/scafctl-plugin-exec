// Package exec implements the exec provider plugin.
//
// The exec provider executes shell commands using an embedded cross-platform
// POSIX shell interpreter (mvdan.cc/sh). Commands work identically on Linux,
// macOS, and Windows without requiring external shell binaries. Supports
// pipes, redirections, variable expansion, and common coreutils on all
// platforms. Optionally use external shells (bash, pwsh, cmd).
package exec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/google/jsonschema-go/jsonschema"
	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
	sdkprovider "github.com/oakwood-commons/scafctl-plugin-sdk/provider"
	sdkhelper "github.com/oakwood-commons/scafctl-plugin-sdk/provider/schemahelper"

	"github.com/oakwood-commons/scafctl-plugin-exec/internal/shellexec"
)

const (
	// ProviderName is the unique identifier for this provider.
	ProviderName = "exec"

	// Version is the provider version.
	Version = "2.0.0"
)

// Plugin implements the scafctl ProviderPlugin interface.
type Plugin struct{}

// NewPlugin creates a new exec plugin instance.
func NewPlugin() *Plugin {
	return &Plugin{}
}

// GetProviders returns the list of providers exposed by this plugin.
//
//nolint:revive // ctx required by interface
func (p *Plugin) GetProviders(_ context.Context) ([]string, error) {
	return []string{ProviderName}, nil
}

// GetProviderDescriptor returns the descriptor for the named provider.
//
//nolint:revive // ctx required by interface
func (p *Plugin) GetProviderDescriptor(_ context.Context, providerName string) (*sdkprovider.Descriptor, error) {
	if providerName != ProviderName {
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}

	return &sdkprovider.Descriptor{
		Name:        ProviderName,
		DisplayName: "Exec Provider",
		Description: "Executes shell commands using an embedded cross-platform POSIX shell interpreter. " +
			"Commands work identically on Linux, macOS, and Windows without requiring external shell binaries. " +
			"Supports pipes, redirections, variable expansion, and common coreutils on all platforms. " +
			"Optionally use external shells (bash, pwsh, cmd) for platform-specific features.",
		APIVersion: "v1",
		Version:    semver.MustParse(Version),
		Category:   "Core",
		Tags:       []string{"exec", "shell", "command", "process"},
		Capabilities: []sdkprovider.Capability{
			sdkprovider.CapabilityAction,
			sdkprovider.CapabilityFrom,
			sdkprovider.CapabilityTransform,
		},
		Schema: sdkhelper.ObjectSchema([]string{"command"}, map[string]*jsonschema.Schema{
			"command": sdkhelper.StringProp("Command to execute. Supports POSIX shell syntax including pipes (|), redirections (>, >>), variable expansion ($VAR), command substitution ($(cmd)), and conditionals by default",
				sdkhelper.WithExample("echo hello | tr a-z A-Z"),
				sdkhelper.WithMaxLength(100000)),
			"args": sdkhelper.ArrayProp("Additional arguments appended to the command. Arguments are automatically shell-quoted for safety",
				sdkhelper.WithMaxItems(100)),
			"stdin": sdkhelper.StringProp("Standard input to provide to the command",
				sdkhelper.WithMaxLength(1000000)),
			"workingDir": sdkhelper.StringProp("Working directory for command execution",
				sdkhelper.WithExample("/tmp"),
				sdkhelper.WithMaxLength(500)),
			"env": sdkhelper.AnyProp("Environment variables to set (key-value pairs). Merged with the parent process environment. NO_COLOR=1 and TERM=dumb are injected automatically to prevent child processes from emitting ANSI escape codes in captured output (override by setting them explicitly)"),
			"timeout": sdkhelper.IntProp("Timeout in seconds (0 or omit for no timeout)",
				sdkhelper.WithExample("30"),
				sdkhelper.WithMaximum(3600.0)),
			"shell": sdkhelper.StringProp(
				"Shell interpreter to use. "+
					"'auto' (default): embedded POSIX shell that works identically on all platforms (Linux, macOS, Windows). "+
					"'sh': alias for 'auto'. "+
					"'bash': external bash binary from PATH. "+
					"'pwsh': external PowerShell Core (pwsh) from PATH. "+
					"'cmd': external cmd.exe (Windows only)",
				sdkhelper.WithEnum("auto", "sh", "bash", "pwsh", "cmd"),
				sdkhelper.WithDefault("auto"),
				sdkhelper.WithExample("auto"),
				sdkhelper.WithMaxLength(10)),
			"raw":         sdkhelper.BoolProp("Return trimmed stdout string instead of the full result map. Only applies in resolver/transform mode; action mode always returns the full map"),
			"passthrough": sdkhelper.BoolProp("Stream stdout/stderr directly to the user's terminal in real-time when terminal IO streams are available instead of capturing. Default: false"),
		}),
		OutputSchemas: map[sdkprovider.Capability]*jsonschema.Schema{
			sdkprovider.CapabilityFrom:      sdkhelper.AnyProp("Full result map (stdout, stderr, exitCode, success, command, shell) by default; trimmed stdout string when raw: true"),
			sdkprovider.CapabilityTransform: sdkhelper.AnyProp("Full result map (stdout, stderr, exitCode, success, command, shell) by default; trimmed stdout string when raw: true"),
			sdkprovider.CapabilityAction: sdkhelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"success":  sdkhelper.BoolProp("Whether the command succeeded (exit code 0)"),
				"stdout":   sdkhelper.StringProp("Standard output from the command"),
				"stderr":   sdkhelper.StringProp("Standard error output from the command"),
				"exitCode": sdkhelper.IntProp("Command exit code"),
				"command":  sdkhelper.StringProp("The full command that was executed"),
				"shell":    sdkhelper.StringProp("The shell interpreter that was used"),
			}),
		},
		Examples: []sdkprovider.Example{
			{
				Name:        "Simple command execution",
				Description: "Execute a simple echo command \u2014 pipes and shell features work by default",
				YAML: "name: echo-hello\nprovider: exec\ninputs:\n  command: echo \"Hello, World!\"",
			},
			{
				Name:        "Command with arguments",
				Description: "Pass explicit arguments that are automatically shell-quoted",
				YAML: "name: echo-args\nprovider: exec\ninputs:\n  command: echo\n  args:\n    - \"Hello\"\n    - \"World\"",
			},
			{
				Name:        "Pipeline command",
				Description: "Use pipes, redirections, and shell features \u2014 works on all platforms",
				YAML: "name: pipeline\nprovider: exec\ninputs:\n  command: \"echo 'hello world' | tr a-z A-Z\"",
			},
			{
				Name:        "Command with timeout",
				Description: "Run a command with a 30 second timeout",
				YAML: "name: curl-with-timeout\nprovider: exec\ninputs:\n  command: curl -s https://api.example.com/data\n  timeout: 30",
			},
			{
				Name:        "Command with custom environment",
				Description: "Execute a script with custom environment variables and working directory",
				YAML: "name: custom-env-script\nprovider: exec\ninputs:\n  command: ./build.sh\n  workingDir: /project/src\n  env:\n    BUILD_ENV: production\n    VERSION: \"1.0.0\"",
			},
			{
				Name:        "PowerShell command",
				Description: "Use PowerShell for Windows-specific operations",
				YAML: "name: pwsh-example\nprovider: exec\ninputs:\n  command: \"Get-ChildItem | Select-Object Name\"\n  shell: pwsh",
			},
			{
				Name:        "External bash",
				Description: "Use an external bash shell for bash-specific features",
				YAML: "name: bash-specific\nprovider: exec\ninputs:\n  command: 'shopt -s globstar; echo **/*.go'\n  shell: bash",
			},
		},
	}, nil
}

// ConfigureProvider stores host-side configuration.
//
//nolint:revive // ctx and cfg required by interface
func (p *Plugin) ConfigureProvider(_ context.Context, _ string, _ sdkplugin.ProviderConfig) error {
	return nil
}

// ExecuteProvider executes the named provider with the given input.
func (p *Plugin) ExecuteProvider(ctx context.Context, providerName string, input map[string]any) (*sdkprovider.Output, error) {
	if providerName != ProviderName {
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}

	lgr := logr.FromContextOrDiscard(ctx)

	command, ok := input["command"].(string)
	if !ok || command == "" {
		return nil, fmt.Errorf("%s: command is required and must be a non-empty string", ProviderName)
	}

	// Parse shell type.
	shell := shellexec.ShellAuto
	if shellRaw, ok := input["shell"]; ok && shellRaw != nil {
		shellStr, ok := shellRaw.(string)
		if !ok {
			return nil, fmt.Errorf("%s: shell must be a string (one of: %s)", ProviderName,
				strings.Join(shellexec.ValidShellTypes(), ", "))
		}
		shell = shellexec.ShellType(shellStr)
		if !shell.IsValid() {
			return nil, fmt.Errorf("%s: unsupported shell type %q, valid values: %s", ProviderName,
				shellStr, strings.Join(shellexec.ValidShellTypes(), ", "))
		}
	}

	lgr.V(1).Info("executing provider", "provider", ProviderName, "command", command, "shell", string(shell))

	// Check for dry-run mode.
	if dryRun := sdkprovider.DryRunFromContext(ctx); dryRun {
		output := executeDryRun(command, input, shell)
		lgr.V(1).Info("provider completed (dry-run)", "provider", ProviderName)
		return output, nil
	}

	output, err := executeCommand(ctx, command, input, shell)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}
	lgr.V(1).Info("provider completed", "provider", ProviderName)
	return output, nil
}

// DescribeWhatIf returns a description of what the provider would do.
//
//nolint:revive // ctx required by interface
func (p *Plugin) DescribeWhatIf(_ context.Context, providerName string, input map[string]any) (string, error) {
	if providerName != ProviderName {
		return "", fmt.Errorf("unknown provider: %s", providerName)
	}

	command, _ := input["command"].(string)
	shell, _ := input["shell"].(string)
	if shell == "" {
		shell = "auto"
	}
	var args []string
	if argsRaw, ok := input["args"]; ok && argsRaw != nil {
		if argSlice, ok := argsRaw.([]any); ok {
			for _, arg := range argSlice {
				args = append(args, fmt.Sprint(arg))
			}
		}
	}
	fullCmd := shellexec.BuildFullCommand(command, args)
	msg := fmt.Sprintf("Would execute via %s shell: %s", shell, fullCmd)
	if workingDir, ok := input["workingDir"].(string); ok && workingDir != "" {
		msg += fmt.Sprintf(" in directory: %s", workingDir)
	}
	return msg, nil
}

// ExecuteProviderStream is not supported by the exec provider.
//
//nolint:revive // all params required by interface
func (p *Plugin) ExecuteProviderStream(_ context.Context, _ string, _ map[string]any, _ func(sdkplugin.StreamChunk)) error {
	return sdkplugin.ErrStreamingNotSupported
}

// ExtractDependencies returns resolver keys this input depends on.
//
//nolint:revive // all params required by interface
func (p *Plugin) ExtractDependencies(_ context.Context, _ string, _ map[string]any) ([]string, error) {
	return nil, nil
}

// StopProvider performs cleanup for the named provider.
//
//nolint:revive // all params required by interface
func (p *Plugin) StopProvider(_ context.Context, _ string) error {
	return nil
}

// executeCommand runs the actual shell command.
func executeCommand(ctx context.Context, command string, inputs map[string]any, shell shellexec.ShellType) (*sdkprovider.Output, error) {
	// Parse arguments.
	var args []string
	if argsRaw, ok := inputs["args"]; ok && argsRaw != nil {
		switch v := argsRaw.(type) {
		case []any:
			for _, arg := range v {
				args = append(args, fmt.Sprint(arg))
			}
		case []string:
			args = v
		default:
			return nil, fmt.Errorf("args must be an array")
		}
	}

	// Parse timeout.
	cmdCtx := ctx
	var cancel context.CancelFunc

	if timeoutRaw, ok := inputs["timeout"]; ok && timeoutRaw != nil {
		var timeoutSecs int
		switch v := timeoutRaw.(type) {
		case int:
			timeoutSecs = v
		case float64:
			timeoutSecs = int(v)
		default:
			return nil, fmt.Errorf("timeout must be an integer")
		}
		if timeoutSecs > 0 {
			cmdCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
			defer cancel()
		}
	}

	// Build environment.
	// Inject NO_COLOR=1 and TERM=dumb to prevent child processes (especially
	// PowerShell) from emitting ANSI escape codes in captured output, which
	// corrupts downstream processing and breaks YAML block-scalar formatting.
	var env []string
	if envRaw, ok := inputs["env"]; ok && envRaw != nil {
		envMap, ok := envRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("env must be an object with string keys")
		}
		if _, exists := envMap["NO_COLOR"]; !exists {
			envMap["NO_COLOR"] = "1"
		}
		if _, exists := envMap["TERM"]; !exists {
			envMap["TERM"] = "dumb"
		}
		env = shellexec.MergeEnv(envMap)
	} else {
		// No user env -- still inject NO_COLOR and TERM.
		env = shellexec.MergeEnv(map[string]any{
			"NO_COLOR": "1",
			"TERM":     "dumb",
		})
	}

	// Set up stdin.
	var stdin *strings.Reader
	if stdinStr, ok := inputs["stdin"].(string); ok && stdinStr != "" {
		stdin = strings.NewReader(stdinStr)
	}

	// Parse working directory.
	var workingDir string
	if dir, ok := inputs["workingDir"].(string); ok && dir != "" {
		resolved, resolveErr := resolvePath(ctx, dir)
		if resolveErr != nil {
			return nil, fmt.Errorf("invalid workingDir: %w", resolveErr)
		}
		workingDir = resolved
	} else {
		// No explicit workingDir: default to output-dir when in action mode.
		if mode, modeOK := sdkprovider.ExecutionModeFromContext(ctx); modeOK && mode == sdkprovider.CapabilityAction {
			if outputDir, dirOK := sdkprovider.OutputDirectoryFromContext(ctx); dirOK && outputDir != "" {
				workingDir = outputDir
			}
		}
	}

	// Set up stdout/stderr writers.
	var stdout, stderr bytes.Buffer
	var stdoutWriter, stderrWriter io.Writer = &stdout, &stderr
	streamed := false
	passthrough, _ := inputs["passthrough"].(bool)

	if passthrough {
		// Passthrough mode: stream directly to terminal, don't capture.
		if ioStreams, ok := sdkprovider.IOStreamsFromContext(ctx); ok && ioStreams != nil {
			if ioStreams.Out != nil {
				stdoutWriter = ioStreams.Out
			}
			if ioStreams.ErrOut != nil {
				stderrWriter = ioStreams.ErrOut
			}
			streamed = true
		}
	} else {
		mode, _ := sdkprovider.ExecutionModeFromContext(ctx)
		if mode == sdkprovider.CapabilityAction {
			if ioStreams, ok := sdkprovider.IOStreamsFromContext(ctx); ok && ioStreams != nil {
				if ioStreams.Out != nil {
					stdoutWriter = io.MultiWriter(&stdout, ioStreams.Out)
					streamed = true
				}
				if ioStreams.ErrOut != nil {
					stderrWriter = io.MultiWriter(&stderr, ioStreams.ErrOut)
				}
			}
		}
	}

	// Build run options.
	opts := &shellexec.RunOptions{
		Command: command,
		Args:    args,
		Shell:   shell,
		Dir:     workingDir,
		Env:     env,
		Stdout:  stdoutWriter,
		Stderr:  stderrWriter,
	}
	if stdin != nil {
		opts.Stdin = stdin
	}

	// Execute command.
	result, err := shellexec.RunWithContext(cmdCtx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}

	// Build full command string for output.
	fullCmd := shellexec.BuildFullCommand(command, args)

	fullResult := map[string]any{
		"stdout":   stdout.String(),
		"stderr":   stderr.String(),
		"exitCode": result.ExitCode,
		"success":  result.ExitCode == 0,
		"command":  fullCmd,
		"shell":    string(result.Shell),
	}

	// In resolver/transform mode with raw: true, return trimmed stdout string.
	raw, _ := inputs["raw"].(bool)
	if raw {
		if mode, modeOK := sdkprovider.ExecutionModeFromContext(ctx); modeOK &&
			(mode == sdkprovider.CapabilityFrom || mode == sdkprovider.CapabilityTransform) {
			return &sdkprovider.Output{
				Data:     strings.TrimSpace(stdout.String()),
				Streamed: streamed,
			}, nil
		}
	}

	return &sdkprovider.Output{
		Data:     fullResult,
		Streamed: streamed,
	}, nil
}

//nolint:unparam // Error return kept for consistent interface
func executeDryRun(command string, inputs map[string]any, shell shellexec.ShellType) *sdkprovider.Output {
	// Parse arguments.
	var args []string
	if argsRaw, ok := inputs["args"]; ok && argsRaw != nil {
		if argSlice, ok := argsRaw.([]any); ok {
			for _, arg := range argSlice {
				args = append(args, fmt.Sprint(arg))
			}
		}
	}

	fullCmd := shellexec.BuildFullCommand(command, args)

	message := fmt.Sprintf("Would execute via %s shell: %s", shell, fullCmd)
	if workingDir, ok := inputs["workingDir"].(string); ok && workingDir != "" {
		message += fmt.Sprintf(" in directory: %s", workingDir)
	}

	return &sdkprovider.Output{
		Data: map[string]any{
			"stdout":   "",
			"stderr":   "",
			"exitCode": 0,
			"success":  true,
			"command":  fullCmd,
			"shell":    string(shell),
			"_dryRun":  true,
			"_message": message,
		},
	}
}

// resolvePath resolves a path relative to the working directory from context.
// Absolute paths are returned as-is; relative paths are resolved against the
// context working directory or the process CWD.
func resolvePath(ctx context.Context, path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}

	base, ok := sdkprovider.WorkingDirectoryFromContext(ctx)
	if !ok || base == "" {
		base, _ = os.Getwd()
	}

	return filepath.Clean(filepath.Join(base, path)), nil
}

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
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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
	Version = "3.0.0"

	// maxStderrInError is the maximum number of stderr runes to include in
	// failOnError error messages.
	maxStderrInError = 512
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
			"raw": sdkhelper.BoolProp(
				"Return trimmed stdout string instead of the full result map. " +
					"Only applies in from/transform mode; action mode always returns the full map. " +
					"Cannot be combined with failOnError: false because raw mode discards exitCode from Output.Data " +
					"(exitCode and stderr remain in Output.Metadata for failOnError diagnostics)"),
			"passthrough": sdkhelper.BoolProp(
				"Stream stdout and stderr to the terminal in real-time while still capturing both in the provider output. " +
					"Defaults to true for action capability and false for from/transform capabilities " +
					"(set to false on an action to buffer output instead)"),
			"echo": sdkhelper.BoolProp("Print the resolved command to stderr before executing. Useful for debugging dynamic commands built with expr:. Default: false"),
			"failOnError": sdkhelper.BoolProp(
				"Return an error to the engine when the command exits with a non-zero exit code. "+
					"Set to false to inspect non-zero exit codes in the structured result map "+
					"(cannot be combined with raw: true in from/transform mode). Default: true",
				sdkhelper.WithDefault(true)),
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
				Description: "Execute a simple echo command -- pipes and shell features work by default",
				YAML:        "name: echo-hello\nprovider: exec\ninputs:\n  command: echo \"Hello, World!\"",
			},
			{
				Name:        "Raw string output",
				Description: "Capture trimmed stdout string in a resolver -- fails fast on non-zero exit by default",
				YAML:        "name: git-sha\nprovider: exec\ninputs:\n  command: git rev-parse --short HEAD\n  raw: true",
			},
			{
				Name:        "Inspect non-zero exit code",
				Description: "Opt into structured exit-code inspection with failOnError: false",
				YAML:        "name: check-tool\nprovider: exec\ninputs:\n  command: which jq\n  failOnError: false",
			},
			{
				Name:        "Command with arguments",
				Description: "Pass explicit arguments that are automatically shell-quoted",
				YAML:        "name: echo-args\nprovider: exec\ninputs:\n  command: echo\n  args:\n    - \"Hello\"\n    - \"World\"",
			},
			{
				Name:        "Pipeline command",
				Description: "Use pipes, redirections, and shell features -- works on all platforms",
				YAML:        "name: pipeline\nprovider: exec\ninputs:\n  command: \"echo 'hello world' | tr a-z A-Z\"",
			},
			{
				Name:        "Command with timeout",
				Description: "Run a command with a 30 second timeout",
				YAML:        "name: curl-with-timeout\nprovider: exec\ninputs:\n  command: curl -s https://api.example.com/data\n  timeout: 30",
			},
			{
				Name:        "Command with custom environment",
				Description: "Execute a script with custom environment variables and working directory",
				YAML:        "name: custom-env-script\nprovider: exec\ninputs:\n  command: ./build.sh\n  workingDir: /project/src\n  env:\n    BUILD_ENV: production\n    VERSION: \"1.0.0\"",
			},
			{
				Name:        "PowerShell command",
				Description: "Use PowerShell for Windows-specific operations",
				YAML:        "name: pwsh-example\nprovider: exec\ninputs:\n  command: \"Get-ChildItem | Select-Object Name\"\n  shell: pwsh",
			},
			{
				Name:        "External bash",
				Description: "Use an external bash shell for bash-specific features",
				YAML:        "name: bash-specific\nprovider: exec\ninputs:\n  command: 'shopt -s globstar; echo **/*.go'\n  shell: bash",
			},
			{
				Name:        "Echo resolved command",
				Description: "Print the resolved command to stderr before executing -- action output streams live by default",
				YAML:        "name: build\nprovider: exec\ninputs:\n  command: go build -trimpath -o dist/app .\n  echo: true",
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

	command, shell, err := validateAndParseInputs(ctx, input)
	if err != nil {
		return nil, err
	}

	lgr.V(1).Info("executing provider", "provider", ProviderName, "command", command, "shell", string(shell))

	// Check for dry-run mode.
	if dryRun := sdkprovider.DryRunFromContext(ctx); dryRun {
		output := executeDryRun(command, input, shell)
		lgr.V(1).Info("provider completed (dry-run)", "provider", ProviderName)
		return output, nil
	}

	output, err := executeCommand(ctx, command, input, shell, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}

	// Check failOnError: if the command exited non-zero and failOnError is
	// active, return both the output data and an error so the engine treats
	// this execution as failed.
	if shouldFailOnError(input) {
		exitCode, stderrStr := extractExitInfo(output)
		if exitCode != 0 {
			return output, fmt.Errorf("%s: command exited with code %d%s",
				ProviderName, exitCode, formatStderrSuffix(stderrStr))
		}
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
	if failOnError, ok := input["failOnError"].(bool); ok {
		if failOnError {
			msg += "; will fail on non-zero exit code"
		} else {
			msg += "; will NOT fail on non-zero exit code"
		}
	}
	return msg, nil
}

// ExecuteProviderStream streams stdout and stderr chunks in real-time when live
// streaming is active (by default for CapabilityAction unless passthrough is
// explicitly false, and for other modes when passthrough is explicitly true).
// Both stdout and stderr remain captured in Result.Data and Output.Streamed is
// set to true whenever live streaming was active.
//
// When live streaming is inactive, the full output is captured and sent as a
// single result chunk (identical behavior to ExecuteProvider).
func (p *Plugin) ExecuteProviderStream(ctx context.Context, providerName string, inputs map[string]any, cb func(sdkplugin.StreamChunk)) error {
	if cb == nil {
		return fmt.Errorf("%s: stream callback must not be nil", ProviderName)
	}
	if inputs == nil {
		inputs = map[string]any{}
	}
	if providerName != ProviderName {
		cb(sdkplugin.StreamChunk{Error: fmt.Sprintf("unknown provider: %s", providerName)})
		return nil
	}

	if !shouldStream(ctx, inputs) {
		// Buffered mode: capture and send as a single result chunk.
		output, err := p.ExecuteProvider(ctx, providerName, inputs)
		if err != nil {
			if output != nil {
				cb(sdkplugin.StreamChunk{Result: output})
			}
			cb(sdkplugin.StreamChunk{Error: err.Error()})
			return nil
		}
		cb(sdkplugin.StreamChunk{Result: output})
		return nil
	}

	command, shell, err := validateAndParseInputs(ctx, inputs)
	if err != nil {
		cb(sdkplugin.StreamChunk{Error: err.Error()})
		return nil
	}

	if sdkprovider.DryRunFromContext(ctx) {
		cb(sdkplugin.StreamChunk{Result: executeDryRun(command, inputs, shell)})
		return nil
	}

	// Stream stdout/stderr chunks as bytes arrive so the host can display them
	// immediately, while executeCommand also captures both streams into Result.Data.
	// A shared mutex serializes concurrent writes across stdout, stderr, and
	// terminal result/error chunks.
	mu := &sync.Mutex{}
	closed := false
	stdoutWriter := &chunkWriter{cb: cb, isStdout: true, mu: mu, closed: &closed}
	stderrWriter := &chunkWriter{cb: cb, isStdout: false, mu: mu, closed: &closed}
	output, err := executeCommand(ctx, command, inputs, shell, stdoutWriter, stderrWriter)

	mu.Lock()
	defer mu.Unlock()
	closed = true

	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		cb(sdkplugin.StreamChunk{Error: fmt.Sprintf("%s: %v", ProviderName, err)})
		return nil
	}

	// Check failOnError for live streaming path.
	if shouldFailOnError(inputs) {
		exitCode, stderrStr := extractExitInfo(output)
		if exitCode != 0 {
			cb(sdkplugin.StreamChunk{Result: output})
			cb(sdkplugin.StreamChunk{Error: fmt.Sprintf("%s: command exited with code %d%s",
				ProviderName, exitCode, formatStderrSuffix(stderrStr))})
			return nil
		}
	}

	cb(sdkplugin.StreamChunk{Result: output})
	return nil
}

// chunkWriter forwards writes as StreamChunk callbacks so the host receives
// output in real-time over the gRPC server-stream. mu serializes concurrent
// writes across stdout, stderr, and terminal chunks; closed suppresses late
// writes after the stream completes.
type chunkWriter struct {
	cb       func(sdkplugin.StreamChunk)
	isStdout bool
	mu       *sync.Mutex
	closed   *bool
}

func (w *chunkWriter) Write(p []byte) (int, error) {
	if len(p) == 0 || w.cb == nil {
		return len(p), nil
	}
	chunk := make([]byte, len(p))
	copy(chunk, p)
	if w.mu != nil {
		w.mu.Lock()
		defer w.mu.Unlock()
	}
	if w.closed != nil && *w.closed {
		return len(p), nil
	}
	if w.isStdout {
		w.cb(sdkplugin.StreamChunk{Stdout: chunk})
	} else {
		w.cb(sdkplugin.StreamChunk{Stderr: chunk})
	}
	return len(p), nil
}

// captureWriter buffers all written bytes for Result.Data while optionally
// forwarding each write to a live stream under the same mutex so concurrent
// subshell goroutines cannot race on capture buffers or interleave stream writes.
type captureWriter struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	forward   io.Writer
	forwardMu *sync.Mutex
}

func (w *captureWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, _ = w.buf.Write(p)
	if w.forward != nil {
		if w.forwardMu != nil {
			w.forwardMu.Lock()
			defer w.forwardMu.Unlock()
		}
		n, err := w.forward.Write(p)
		if err != nil {
			return n, err
		}
		if n != len(p) {
			return n, io.ErrShortWrite
		}
	}
	return len(p), nil
}

// WriteForward writes s directly to dst under the capture and forward mutexes
// without recording s in the capture buffer.
func (w *captureWriter) WriteForward(dst io.Writer, s string) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.forwardMu != nil {
		w.forwardMu.Lock()
		defer w.forwardMu.Unlock()
	}
	return io.WriteString(dst, s)
}

// StopForward detaches any live forward writer once command execution finishes
// so an un-waited background subshell cannot mutate caller-owned streams.
func (w *captureWriter) StopForward() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.forward = nil
}

func (w *captureWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.buf.String()
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

// validateAndParseInputs validates provider inputs and resolves command and shell.
func validateAndParseInputs(ctx context.Context, input map[string]any) (string, shellexec.ShellType, error) {
	command, ok := input["command"].(string)
	if !ok || command == "" {
		return "", "", fmt.Errorf("%s: command is required and must be a non-empty string", ProviderName)
	}

	shell := shellexec.ShellAuto
	if shellRaw, ok := input["shell"]; ok && shellRaw != nil {
		shellStr, ok := shellRaw.(string)
		if !ok {
			return "", "", fmt.Errorf("%s: shell must be a string (one of: %s)", ProviderName,
				strings.Join(shellexec.ValidShellTypes(), ", "))
		}
		shell = shellexec.ShellType(shellStr)
		if !shell.IsValid() {
			return "", "", fmt.Errorf("%s: unsupported shell type %q, valid values: %s", ProviderName,
				shellStr, strings.Join(shellexec.ValidShellTypes(), ", "))
		}
	}

	raw, _ := input["raw"].(bool)
	if failOnError, hasFailOnError := input["failOnError"].(bool); raw && hasFailOnError && !failOnError {
		if mode, modeOK := sdkprovider.ExecutionModeFromContext(ctx); modeOK &&
			(mode == sdkprovider.CapabilityFrom || mode == sdkprovider.CapabilityTransform) {
			return "", "", fmt.Errorf(
				"%s: raw: true cannot be combined with failOnError: false in %s mode because raw output discards exitCode",
				ProviderName, mode)
		}
	}

	return command, shell, nil
}

// executeCommand runs the actual shell command. When streamOut or streamErr are
// non-nil (from ExecuteProviderStream), live output is tee'd to those writers
// while still capturing stdout and stderr in fullResult.
func executeCommand(ctx context.Context, command string, inputs map[string]any, shell shellexec.ShellType, streamOut, streamErr io.Writer) (*sdkprovider.Output, error) {
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
	//
	// userEnv holds a snapshot of user-provided keys (before NO_COLOR/TERM
	// injection) so that echo: true can print only what the user specified.
	var env []string
	var userEnv map[string]any
	if envRaw, ok := inputs["env"]; ok && envRaw != nil {
		envMap, ok := envRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("env must be an object with string keys")
		}
		// Snapshot user keys before injecting runtime-only vars.
		userEnv = make(map[string]any, len(envMap))
		for k, v := range envMap {
			userEnv[k] = v
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

	// Set up stdout/stderr writers. Both streams are always captured into
	// mutex-synchronized writers so Result.Data (fullResult) retains complete
	// output without data races even when parallel subshell jobs write
	// concurrently or live streaming is active.
	var forwardOut, forwardErr io.Writer
	streamed := false

	if streamOut != nil || streamErr != nil {
		streamed = true
		forwardOut = streamOut
		forwardErr = streamErr
	} else if shouldStream(ctx, inputs) {
		if ioStreams, ok := sdkprovider.IOStreamsFromContext(ctx); ok && ioStreams != nil {
			if ioStreams.Out != nil {
				forwardOut = ioStreams.Out
				streamed = true
			}
			if ioStreams.ErrOut != nil {
				forwardErr = ioStreams.ErrOut
				streamed = true
			}
		} else if passthrough, ok := inputs["passthrough"].(bool); ok && passthrough {
			// Explicit passthrough with no host IO streams: tee directly to process
			// stdout/stderr so long-running processes display output in real-time.
			forwardOut = os.Stdout
			forwardErr = os.Stderr
			streamed = true
		}
	}

	var forwardMu sync.Mutex
	stdout := &captureWriter{forward: forwardOut, forwardMu: &forwardMu}
	stderr := &captureWriter{forward: forwardErr, forwardMu: &forwardMu}

	// Echo option: print the resolved command to stderr before executing.
	// Always writes to the terminal stderr (streamErr, ioStreams.ErrOut, or os.Stderr),
	// never to the capture buffer, so it doesn't pollute data["stderr"].
	if echo, _ := inputs["echo"].(bool); echo {
		echoOut := forwardErr
		if echoOut == nil {
			if ioStreams, ok := sdkprovider.IOStreamsFromContext(ctx); ok && ioStreams != nil && ioStreams.ErrOut != nil {
				echoOut = ioStreams.ErrOut
			} else {
				echoOut = os.Stderr
			}
		}
		var sb strings.Builder
		sb.WriteString("> ")
		if len(userEnv) > 0 {
			keys := make([]string, 0, len(userEnv))
			for k := range userEnv {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				v := fmt.Sprint(userEnv[k])
				if strings.ContainsAny(v, " \t\"'\\") {
					fmt.Fprintf(&sb, "%s=%q ", k, v)
				} else {
					fmt.Fprintf(&sb, "%s=%s ", k, v)
				}
			}
		}
		sb.WriteString(shellexec.BuildFullCommand(command, args))
		sb.WriteString("\n")
		_, _ = stderr.WriteForward(echoOut, sb.String())
	}

	// Build run options.
	opts := &shellexec.RunOptions{
		Command: command,
		Args:    args,
		Shell:   shell,
		Dir:     workingDir,
		Env:     env,
		Stdout:  stdout,
		Stderr:  stderr,
	}
	if stdin != nil {
		opts.Stdin = stdin
	}

	// Execute command.
	result, err := shellexec.RunWithContext(cmdCtx, opts)
	stdout.StopForward()
	stderr.StopForward()
	if err != nil {
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}

	// Build full command string for output.
	fullCmd := shellexec.BuildFullCommand(command, args)
	stdoutStr := stdout.String()
	stderrStr := stderr.String()

	fullResult := map[string]any{
		"stdout":   stdoutStr,
		"stderr":   stderrStr,
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
				Data: strings.TrimSpace(stdoutStr),
				Metadata: map[string]any{
					"exitCode": result.ExitCode,
					"stderr":   stderrStr,
				},
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

// shouldStream determines whether stdout and stderr should be streamed live.
// Explicit passthrough takes precedence; otherwise CapabilityAction streams by
// default while from, transform, and unset modes buffer by default.
func shouldStream(ctx context.Context, inputs map[string]any) bool {
	if explicit, ok := inputs["passthrough"].(bool); ok {
		return explicit
	}
	mode, ok := sdkprovider.ExecutionModeFromContext(ctx)
	return ok && mode == sdkprovider.CapabilityAction
}

// shouldFailOnError determines whether the provider should return an error for
// non-zero exit codes. Defaults to true across all capabilities unless the
// caller explicitly sets failOnError: false.
func shouldFailOnError(inputs map[string]any) bool {
	if explicit, ok := inputs["failOnError"].(bool); ok {
		return explicit
	}
	return true
}

// extractExitInfo reads the exit code and stderr from a provider output.
// It checks Data (map[string]any) first, then falls back to Metadata
// (used when raw: true returns Data as a string).
func extractExitInfo(output *sdkprovider.Output) (int, string) {
	if output == nil {
		return 0, ""
	}
	if data, ok := output.Data.(map[string]any); ok {
		exitCode, _ := data["exitCode"].(int)
		stderrStr, _ := data["stderr"].(string)
		return exitCode, stderrStr
	}
	if output.Metadata != nil {
		exitCode, _ := output.Metadata["exitCode"].(int)
		stderrStr, _ := output.Metadata["stderr"].(string)
		return exitCode, stderrStr
	}
	return 0, ""
}

// formatStderrSuffix returns a suffix string containing truncated stderr for
// inclusion in error messages. Returns empty string if stderr is empty.
// Truncation is rune-aware to avoid splitting multi-byte UTF-8 characters.
func formatStderrSuffix(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	if utf8.RuneCountInString(stderr) > maxStderrInError {
		var byteIdx int
		for i := 0; i < maxStderrInError; i++ {
			_, size := utf8.DecodeRuneInString(stderr[byteIdx:])
			byteIdx += size
		}
		stderr = stderr[:byteIdx] + "..."
	}
	return ": " + stderr
}

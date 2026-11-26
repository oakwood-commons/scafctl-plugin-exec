// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
	sdkprovider "github.com/oakwood-commons/scafctl-plugin-sdk/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl-plugin-exec/internal/shellexec"
)

func TestGetProviders(t *testing.T) {
	p := NewPlugin()
	providers, err := p.GetProviders(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{ProviderName}, providers)
}

func TestGetProviderDescriptor(t *testing.T) {
	p := NewPlugin()
	d, err := p.GetProviderDescriptor(context.Background(), ProviderName)
	require.NoError(t, err)
	require.NoError(t, sdkprovider.ValidateDescriptor(d))
	assert.Equal(t, ProviderName, d.Name)
	assert.Equal(t, "Exec Provider", d.DisplayName)
	assert.Equal(t, "v1", d.APIVersion)
	assert.Equal(t, "3.0.0", d.Version.String())
	assert.NotNil(t, d.Schema)
	assert.Contains(t, d.Capabilities, sdkprovider.CapabilityAction)
	assert.Contains(t, d.Capabilities, sdkprovider.CapabilityFrom)
	assert.Contains(t, d.Capabilities, sdkprovider.CapabilityTransform)
	assert.Contains(t, d.Schema.Required, "command")
	assert.NotEmpty(t, d.Examples)
	assert.NotNil(t, d.OutputSchemas[sdkprovider.CapabilityAction])

	failOnErrorProp := d.Schema.Properties["failOnError"]
	require.NotNil(t, failOnErrorProp)
	assert.Equal(t, json.RawMessage("true"), failOnErrorProp.Default)

	passthroughProp := d.Schema.Properties["passthrough"]
	require.NotNil(t, passthroughProp)
	assert.Empty(t, passthroughProp.Default)
	assert.Contains(t, passthroughProp.Description, "action")
}

func TestGetProviderDescriptor_UnknownProvider(t *testing.T) {
	p := NewPlugin()
	_, err := p.GetProviderDescriptor(context.Background(), "unknown")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}

func TestExecuteProvider_SimpleCommand(t *testing.T) {
	p := NewPlugin()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "echo",
		"args":    []any{"hello", "world"},
	}

	output, err := p.ExecuteProvider(ctx, ProviderName, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "hello world\n", data["stdout"])
	assert.Equal(t, "", data["stderr"])
	assert.Equal(t, 0, data["exitCode"])
	assert.Equal(t, true, data["success"])
	assert.Equal(t, "echo 'hello' 'world'", data["command"])
	assert.NotEmpty(t, data["shell"])
}

func TestExecuteProvider_NoArgs(t *testing.T) {
	p := NewPlugin()

	inputs := map[string]any{
		"command": "pwd",
	}

	output, err := p.ExecuteProvider(context.Background(), ProviderName, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.NotEmpty(t, data["stdout"])
	assert.Equal(t, 0, data["exitCode"])
	assert.Equal(t, true, data["success"])
}

func TestExecuteProvider_WithStdin(t *testing.T) {
	p := NewPlugin()

	inputs := map[string]any{
		"command": "cat",
		"stdin":   "test input",
	}

	output, err := p.ExecuteProvider(context.Background(), ProviderName, inputs)
	require.NoError(t, err)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "test input", data["stdout"])
}

func TestExecuteProvider_WithWorkingDir(t *testing.T) {
	p := NewPlugin()

	inputs := map[string]any{
		"command":    "pwd",
		"workingDir": "/tmp",
	}

	output, err := p.ExecuteProvider(context.Background(), ProviderName, inputs)
	require.NoError(t, err)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, data["stdout"], "/tmp")
}

func TestExecuteProvider_WithEnv(t *testing.T) {
	p := NewPlugin()

	inputs := map[string]any{
		"command": "echo $TEST_VAR",
		"env": map[string]any{
			"TEST_VAR": "test_value",
		},
	}

	output, err := p.ExecuteProvider(context.Background(), ProviderName, inputs)
	require.NoError(t, err)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "test_value\n", data["stdout"])
}

func TestExecuteProvider_NonZeroExitCode(t *testing.T) {
	p := NewPlugin()

	inputs := map[string]any{
		"command": "exit 42",
	}

	output, err := p.ExecuteProvider(context.Background(), ProviderName, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command exited with code 42")
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 42, data["exitCode"])
	assert.Equal(t, false, data["success"])
}

func TestExecuteProvider_StderrOutput(t *testing.T) {
	p := NewPlugin()

	inputs := map[string]any{
		"command": "echo error message >&2",
	}

	output, err := p.ExecuteProvider(context.Background(), ProviderName, inputs)
	require.NoError(t, err)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "error message\n", data["stderr"])
	assert.Equal(t, "", data["stdout"])
}

func TestExecuteProvider_WithShellAuto(t *testing.T) {
	p := NewPlugin()

	inputs := map[string]any{
		"command": "echo hello",
		"shell":   "auto",
	}

	output, err := p.ExecuteProvider(context.Background(), ProviderName, inputs)
	require.NoError(t, err)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "hello\n", data["stdout"])
	assert.Equal(t, "auto", data["shell"])
}

func TestExecuteProvider_WithShellSh(t *testing.T) {
	p := NewPlugin()

	inputs := map[string]any{
		"command": "echo hello",
		"shell":   "sh",
	}

	output, err := p.ExecuteProvider(context.Background(), ProviderName, inputs)
	require.NoError(t, err)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "hello\n", data["stdout"])
	assert.Equal(t, "sh", data["shell"])
}

func TestExecuteProvider_InvalidShellType(t *testing.T) {
	p := NewPlugin()

	_, err := p.ExecuteProvider(context.Background(), ProviderName, map[string]any{
		"command": "echo hello",
		"shell":   "zsh",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported shell type")
}

func TestExecuteProvider_ShellNotString(t *testing.T) {
	p := NewPlugin()

	_, err := p.ExecuteProvider(context.Background(), ProviderName, map[string]any{
		"command": "echo hello",
		"shell":   true,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shell must be a string")
}

func TestExecuteProvider_Pipeline(t *testing.T) {
	p := NewPlugin()

	inputs := map[string]any{
		"command": "echo 'hello world' | tr a-z A-Z",
	}

	output, err := p.ExecuteProvider(context.Background(), ProviderName, inputs)
	require.NoError(t, err)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "HELLO WORLD\n", data["stdout"])
}

func TestExecuteProvider_WithTimeout(t *testing.T) {
	p := NewPlugin()

	inputs := map[string]any{
		"command": "for i in 1 2 3 4 5 6 7 8 9 10; do echo $i; sleep 1; done",
		"timeout": 2,
	}

	output, err := p.ExecuteProvider(context.Background(), ProviderName, inputs)
	require.Error(t, err)

	errStr := err.Error()
	assert.True(t,
		strings.Contains(errStr, "context") ||
			strings.Contains(errStr, "signal") ||
			strings.Contains(errStr, "command exited with code"),
		"Expected timeout/signal/exit-code error, got: %s", errStr)

	if output != nil {
		data, ok := output.Data.(map[string]any)
		require.True(t, ok)
		exitCode := data["exitCode"].(int)
		assert.NotEqual(t, 0, exitCode, "Expected non-zero exit code from killed process")
	}
}

func TestExecuteProvider_CommandNotFound(t *testing.T) {
	p := NewPlugin()

	output, err := p.ExecuteProvider(context.Background(), ProviderName, map[string]any{
		"command": "nonexistentcommand12345",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command exited with code 127")
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 127, data["exitCode"])
	assert.Equal(t, false, data["success"])
}

func TestExecuteProvider_MissingCommand(t *testing.T) {
	p := NewPlugin()

	_, err := p.ExecuteProvider(context.Background(), ProviderName, map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command is required")
}

func TestExecuteProvider_EmptyCommand(t *testing.T) {
	p := NewPlugin()

	_, err := p.ExecuteProvider(context.Background(), ProviderName, map[string]any{
		"command": "",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command is required")
}

func TestExecuteProvider_InvalidArgs(t *testing.T) {
	p := NewPlugin()

	_, err := p.ExecuteProvider(context.Background(), ProviderName, map[string]any{
		"command": "echo",
		"args":    "not an array",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "args must be an array")
}

func TestExecuteProvider_InvalidEnv(t *testing.T) {
	p := NewPlugin()

	_, err := p.ExecuteProvider(context.Background(), ProviderName, map[string]any{
		"command": "echo",
		"env":     "not an object",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "env must be an object")
}

func TestExecuteProvider_InvalidTimeout(t *testing.T) {
	p := NewPlugin()

	_, err := p.ExecuteProvider(context.Background(), ProviderName, map[string]any{
		"command": "echo",
		"timeout": "not a number",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout must be an integer")
}

func TestExecuteProvider_DryRun(t *testing.T) {
	p := NewPlugin()
	ctx := sdkprovider.WithDryRun(context.Background(), true)

	inputs := map[string]any{
		"command": "echo",
		"args":    []any{"hello"},
	}

	output, err := p.ExecuteProvider(ctx, ProviderName, inputs)
	require.NoError(t, err)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "", data["stdout"])
	assert.Equal(t, 0, data["exitCode"])
	assert.Equal(t, true, data["success"])
	assert.Equal(t, "echo 'hello'", data["command"])
	assert.Equal(t, true, data["_dryRun"])
	assert.Contains(t, data["_message"], "Would execute via auto shell: echo 'hello'")
	assert.Equal(t, "auto", data["shell"])
}

func TestExecuteProvider_DryRun_WithWorkingDir(t *testing.T) {
	p := NewPlugin()
	ctx := sdkprovider.WithDryRun(context.Background(), true)

	inputs := map[string]any{
		"command":    "pwd",
		"workingDir": "/tmp",
	}

	output, err := p.ExecuteProvider(ctx, ProviderName, inputs)
	require.NoError(t, err)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, data["_dryRun"])
	assert.Contains(t, data["_message"], "in directory: /tmp")
}

func TestExecuteProvider_ArgsWithStrings(t *testing.T) {
	p := NewPlugin()

	inputs := map[string]any{
		"command": "echo",
		"args":    []string{"hello", "world"},
	}

	output, err := p.ExecuteProvider(context.Background(), ProviderName, inputs)
	require.NoError(t, err)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "hello world\n", data["stdout"])
}

func TestExecuteProvider_ContextCancellation(t *testing.T) {
	p := NewPlugin()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	inputs := map[string]any{
		"command": "sleep 30",
	}

	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	output, err := p.ExecuteProvider(ctx, ProviderName, inputs)
	elapsed := time.Since(start)

	require.Error(t, err)
	errStr := err.Error()
	assert.True(t,
		strings.Contains(errStr, "context") ||
			strings.Contains(errStr, "signal") ||
			strings.Contains(errStr, "command exited with code"),
		"Expected context, signal, or non-zero exit error, got: %s", errStr)

	if output != nil {
		data, ok := output.Data.(map[string]any)
		require.True(t, ok)
		exitCode := data["exitCode"].(int)
		assert.NotEqual(t, 0, exitCode)
	}

	assert.Less(t, elapsed, 10*time.Second)
}

func TestExecuteProvider_UnknownProvider(t *testing.T) {
	p := NewPlugin()
	_, err := p.ExecuteProvider(context.Background(), "unknown", map[string]any{"command": "echo"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}

func TestExecuteProvider_CommandSubstitution(t *testing.T) {
	p := NewPlugin()

	output, err := p.ExecuteProvider(context.Background(), ProviderName, map[string]any{
		"command": "echo $(echo nested)",
	})
	require.NoError(t, err)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "nested\n", data["stdout"])
}

func TestExecuteProvider_Conditionals(t *testing.T) {
	p := NewPlugin()

	output, err := p.ExecuteProvider(context.Background(), ProviderName, map[string]any{
		"command": "if true; then echo yes; else echo no; fi",
	})
	require.NoError(t, err)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "yes\n", data["stdout"])
}

func TestExecuteProvider_RawTrue_ReturnsTrimmedString(t *testing.T) {
	p := NewPlugin()
	ctx := sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityFrom)

	output, err := p.ExecuteProvider(ctx, ProviderName, map[string]any{
		"command": "echo",
		"args":    []any{"hello"},
		"raw":     true,
	})
	require.NoError(t, err)

	data, ok := output.Data.(string)
	require.True(t, ok, "expected string, got %T", output.Data)
	assert.Equal(t, "hello", data)
}

func TestExecuteProvider_ActionMode_ReturnsFullMap(t *testing.T) {
	p := NewPlugin()
	ctx := sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityAction)

	output, err := p.ExecuteProvider(ctx, ProviderName, map[string]any{
		"command": "echo",
		"args":    []any{"hello"},
	})
	require.NoError(t, err)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "hello\n", data["stdout"])
	assert.Equal(t, true, data["success"])
}

func TestExecuteProvider_Passthrough(t *testing.T) {
	p := NewPlugin()

	var termOut bytes.Buffer
	var termErr bytes.Buffer
	ctx := sdkprovider.WithIOStreams(context.Background(), &sdkprovider.IOStreams{
		Out:    &termOut,
		ErrOut: &termErr,
	})

	output, err := p.ExecuteProvider(ctx, ProviderName, map[string]any{
		"command":     "echo",
		"args":        []any{"passthrough-test"},
		"passthrough": true,
	})
	require.NoError(t, err)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "passthrough-test\n", data["stdout"], "passthrough should still capture stdout in result")
	assert.Equal(t, 0, data["exitCode"])
	assert.Equal(t, true, data["success"])
	assert.True(t, output.Streamed)

	assert.Contains(t, termOut.String(), "passthrough-test")
}

func TestDescribeWhatIf(t *testing.T) {
	p := NewPlugin()

	tests := []struct {
		name     string
		input    map[string]any
		contains string
	}{
		{
			name:     "simple command",
			input:    map[string]any{"command": "echo hello"},
			contains: "Would execute via auto shell: echo hello",
		},
		{
			name:     "with working dir",
			input:    map[string]any{"command": "pwd", "workingDir": "/tmp"},
			contains: "in directory: /tmp",
		},
		{
			name:     "with shell",
			input:    map[string]any{"command": "echo", "shell": "bash"},
			contains: "Would execute via bash shell",
		},
		{
			name:     "with args",
			input:    map[string]any{"command": "echo", "args": []any{"hello"}},
			contains: "echo 'hello'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desc, err := p.DescribeWhatIf(context.Background(), ProviderName, tt.input)
			require.NoError(t, err)
			assert.Contains(t, desc, tt.contains)
		})
	}
}

func TestDescribeWhatIf_UnknownProvider(t *testing.T) {
	p := NewPlugin()
	_, err := p.DescribeWhatIf(context.Background(), "unknown", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}

func TestExecuteProviderStream_NonPassthrough_SendsResultChunk(t *testing.T) {
	p := NewPlugin()
	var chunks []sdkplugin.StreamChunk
	err := p.ExecuteProviderStream(context.Background(), ProviderName, map[string]any{
		"command": "echo hello",
		"shell":   "sh",
	}, func(c sdkplugin.StreamChunk) {
		chunks = append(chunks, c)
	})
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	assert.NotNil(t, chunks[0].Result)
	assert.NotNil(t, chunks[0].Result.Data, "non-passthrough result must carry data")
	data, ok := chunks[0].Result.Data.(map[string]any)
	require.True(t, ok, "result data must be map[string]any")
	assert.Equal(t, "hello\n", data["stdout"])
}

func TestExecuteProviderStream_Passthrough_StreamsChunks(t *testing.T) {
	p := NewPlugin()
	var stdoutData []byte
	err := p.ExecuteProviderStream(context.Background(), ProviderName, map[string]any{
		"command":     "printf 'a\nb\n'",
		"shell":       "sh",
		"passthrough": true,
	}, func(c sdkplugin.StreamChunk) {
		stdoutData = append(stdoutData, c.Stdout...)
	})
	require.NoError(t, err)
	assert.Contains(t, string(stdoutData), "a")
}

func TestExecuteProviderStream_NilCallback_ReturnsError(t *testing.T) {
	p := NewPlugin()
	err := p.ExecuteProviderStream(context.Background(), ProviderName, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "callback must not be nil")
}

func TestExecuteProviderStream_UnknownProvider_SendsErrorChunk(t *testing.T) {
	p := NewPlugin()
	var errChunk string
	err := p.ExecuteProviderStream(context.Background(), "unknown", map[string]any{}, func(c sdkplugin.StreamChunk) {
		errChunk = c.Error
	})
	require.NoError(t, err)
	assert.Contains(t, errChunk, "unknown provider")
}

func TestExecuteProviderStream_NilInputs_NoError(t *testing.T) {
	p := NewPlugin()
	var errChunk string
	_ = p.ExecuteProviderStream(context.Background(), ProviderName, nil, func(c sdkplugin.StreamChunk) {
		if c.Error != "" {
			errChunk = c.Error
		}
	})
	// nil inputs → empty command → error chunk (not a panic)
	assert.NotEmpty(t, errChunk)
}

func TestExtractDependencies(t *testing.T) {
	p := NewPlugin()
	deps, err := p.ExtractDependencies(context.Background(), ProviderName, nil)
	require.NoError(t, err)
	assert.Nil(t, deps)
}

func TestStopProvider(t *testing.T) {
	p := NewPlugin()
	err := p.StopProvider(context.Background(), ProviderName)
	require.NoError(t, err)
}

func TestPluginInterface(_ *testing.T) {
	var _ sdkplugin.ProviderPlugin = (*Plugin)(nil)
}

func TestResolvePath(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		path string
	}{
		{
			name: "absolute path unchanged",
			ctx:  context.Background(),
			path: "/tmp/test",
		},
		{
			name: "relative path resolved against context working dir",
			ctx:  sdkprovider.WithWorkingDirectory(context.Background(), "/base"),
			path: "sub/dir",
		},
		{
			name: "relative path with no context uses cwd",
			ctx:  context.Background(),
			path: "relative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := resolvePath(tt.ctx, tt.path)
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(result, "/"), "expected absolute path, got: %s", result)
		})
	}
}

func TestExecuteProvider_InjectsNoColor(t *testing.T) {
	p := NewPlugin()
	ctx := context.Background()

	// Without user-provided env, NO_COLOR and TERM should still be injected.
	inputs := map[string]any{
		"command": "echo NO_COLOR=$NO_COLOR TERM=$TERM",
	}

	output, err := p.ExecuteProvider(ctx, ProviderName, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, data["stdout"], "NO_COLOR=1")
	assert.Contains(t, data["stdout"], "TERM=dumb")
}

func TestExecuteProvider_InjectsNoColor_WithUserEnv(t *testing.T) {
	p := NewPlugin()
	ctx := context.Background()

	// User provides env but not NO_COLOR -- it should be injected.
	inputs := map[string]any{
		"command": "echo NO_COLOR=$NO_COLOR TERM=$TERM MY_VAR=$MY_VAR",
		"env": map[string]any{
			"MY_VAR": "hello",
		},
	}

	output, err := p.ExecuteProvider(ctx, ProviderName, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, data["stdout"], "NO_COLOR=1")
	assert.Contains(t, data["stdout"], "TERM=dumb")
	assert.Contains(t, data["stdout"], "MY_VAR=hello")
}

func TestExecuteProvider_UserCanOverrideNoColor(t *testing.T) {
	p := NewPlugin()
	ctx := context.Background()

	// User explicitly sets NO_COLOR -- it should not be overridden.
	inputs := map[string]any{
		"command": "echo NO_COLOR=$NO_COLOR",
		"env": map[string]any{
			"NO_COLOR": "",
		},
	}

	output, err := p.ExecuteProvider(ctx, ProviderName, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, data["stdout"], "NO_COLOR=")
	// Ensure it's the user's empty value, not "1".
	assert.NotContains(t, data["stdout"], "NO_COLOR=1")
}

func TestExecuteProvider_Echo_WritesToStderr(t *testing.T) {
	p := NewPlugin()

	var termErr bytes.Buffer
	ctx := sdkprovider.WithIOStreams(context.Background(), &sdkprovider.IOStreams{
		ErrOut: &termErr,
	})

	_, err := p.ExecuteProvider(ctx, ProviderName, map[string]any{
		"command": "echo hello",
		"echo":    true,
	})
	require.NoError(t, err)
	assert.Contains(t, termErr.String(), "> echo hello\n")
}

func TestExecuteProvider_Echo_WithEnv(t *testing.T) {
	p := NewPlugin()

	var termErr bytes.Buffer
	ctx := sdkprovider.WithIOStreams(context.Background(), &sdkprovider.IOStreams{
		ErrOut: &termErr,
	})

	_, err := p.ExecuteProvider(ctx, ProviderName, map[string]any{
		"command": "echo hello",
		"echo":    true,
		"env": map[string]any{
			"CGO_ENABLED": "0",
			"GOOS":        "linux",
		},
	})
	require.NoError(t, err)
	// Env vars should be sorted and prepended before the command.
	assert.Contains(t, termErr.String(), "> CGO_ENABLED=0 GOOS=linux echo hello\n")
}

func TestExecuteProvider_Echo_False_NoPrefix(t *testing.T) {
	p := NewPlugin()

	var termErr bytes.Buffer
	ctx := sdkprovider.WithIOStreams(context.Background(), &sdkprovider.IOStreams{
		ErrOut: &termErr,
	})

	_, err := p.ExecuteProvider(ctx, ProviderName, map[string]any{
		"command": "echo hello",
		"echo":    false,
	})
	require.NoError(t, err)
	assert.NotContains(t, termErr.String(), "> ")
}

func TestExecuteProvider_Passthrough_NoIOStreams_UsesOsStreams(t *testing.T) {
	p := NewPlugin()

	var capturedStdout, capturedStderr io.Writer
	mockFn := shellexec.RunFunc(func(_ context.Context, opts *shellexec.RunOptions) (*shellexec.RunResult, error) {
		capturedStdout = opts.Stdout
		capturedStderr = opts.Stderr
		return &shellexec.RunResult{ExitCode: 0, Shell: shellexec.ShellAuto}, nil
	})

	// No IOStreams in context -- passthrough wraps os.Stdout and os.Stderr in
	// synchronized capture writers so output streams in real-time while remaining
	// captured in Result.Data.
	ctx := shellexec.WithRunFunc(context.Background(), mockFn)

	output, err := p.ExecuteProvider(ctx, ProviderName, map[string]any{
		"command":     "echo hello",
		"passthrough": true,
	})
	require.NoError(t, err)
	assert.NotNil(t, capturedStdout, "stdout writer should be set")
	assert.NotEqual(t, capturedStdout, os.Stdout, "stdout should be wrapped in captureWriter")
	assert.NotNil(t, capturedStderr, "stderr writer should be set")
	assert.NotEqual(t, capturedStderr, os.Stderr, "stderr should be wrapped in captureWriter")
	assert.True(t, output.Streamed)
}

// Integration tests: real commands, real IO wiring — prove the features
// behave correctly end-to-end without any mocking.

// TestIntegration_Echo_OnlyGoesToErrOut verifies that echo: true writes the
// command prefix to ioStreams.ErrOut and NOT to data["stderr"], so callers
// don't see the prefix in captured output.
func TestIntegration_Echo_OnlyGoesToErrOut(t *testing.T) {
	p := NewPlugin()

	var termErr bytes.Buffer
	ctx := sdkprovider.WithIOStreams(context.Background(), &sdkprovider.IOStreams{
		ErrOut: &termErr,
	})

	output, err := p.ExecuteProvider(ctx, ProviderName, map[string]any{
		"command": "echo hello",
		"echo":    true,
	})
	require.NoError(t, err)
	data, ok := output.Data.(map[string]any)
	require.True(t, ok)

	// Echo prefix must appear in the terminal stream.
	assert.Contains(t, termErr.String(), "> echo hello\n", "echo prefix should go to ErrOut")
	// Echo prefix must NOT contaminate the captured stderr returned to the caller.
	assert.Empty(t, data["stderr"], "data[stderr] must not contain the echo prefix")
	// The command's actual stdout must be correct.
	assert.Equal(t, "hello\n", data["stdout"])
}

// TestIntegration_Echo_EnvVarsInPrefix verifies that env vars are shown in
// sorted order in the echo prefix using only the user-provided vars (not the
// auto-injected NO_COLOR/TERM).
func TestIntegration_Echo_EnvVarsInPrefix(t *testing.T) {
	p := NewPlugin()

	var termErr bytes.Buffer
	ctx := sdkprovider.WithIOStreams(context.Background(), &sdkprovider.IOStreams{
		ErrOut: &termErr,
	})

	output, err := p.ExecuteProvider(ctx, ProviderName, map[string]any{
		"command": "echo $MY_VAR",
		"echo":    true,
		"env": map[string]any{
			"MY_VAR": "world",
		},
	})
	require.NoError(t, err)
	data, ok := output.Data.(map[string]any)
	require.True(t, ok)

	// Only the user-specified var should appear in the prefix — not NO_COLOR or TERM.
	assert.Contains(t, termErr.String(), "> MY_VAR=world echo $MY_VAR\n")
	assert.NotContains(t, termErr.String(), "NO_COLOR", "auto-injected vars must not appear in echo prefix")
	assert.NotContains(t, termErr.String(), "TERM=", "auto-injected vars must not appear in echo prefix")
	// Command must still execute correctly with the env var set.
	assert.Equal(t, "world\n", data["stdout"])
}

// TestIntegration_Passthrough_StreamsToIOStreams verifies that passthrough: true
// with ioStreams.Out set causes output to flow into the provided writer in
// real-time, and that output.Streamed is true.
func TestIntegration_Passthrough_StreamsToIOStreams(t *testing.T) {
	p := NewPlugin()

	var streamBuf bytes.Buffer
	ctx := sdkprovider.WithIOStreams(context.Background(), &sdkprovider.IOStreams{
		Out: &streamBuf,
	})

	output, err := p.ExecuteProvider(ctx, ProviderName, map[string]any{
		"command":     "echo streamed-output",
		"passthrough": true,
	})
	require.NoError(t, err)
	data, ok := output.Data.(map[string]any)
	require.True(t, ok)

	// In passthrough mode the command output goes to ioStreams.Out AND is captured in data["stdout"].
	assert.Equal(t, "streamed-output\n", streamBuf.String(), "output must appear in ioStreams.Out")
	assert.True(t, output.Streamed, "output.Streamed must be true in passthrough mode")
	assert.Equal(t, "streamed-output\n", data["stdout"], "data[stdout] should remain captured alongside streaming")
}

// TestIntegration_ActionMode_MultiWriter verifies that in action mode (non-passthrough)
// with ioStreams.Out set, output is written to BOTH the stream and the captured buffer.
func TestIntegration_ActionMode_MultiWriter(t *testing.T) {
	p := NewPlugin()

	var streamBuf bytes.Buffer
	ctx := sdkprovider.WithIOStreams(context.Background(), &sdkprovider.IOStreams{
		Out: &streamBuf,
	})
	ctx = sdkprovider.WithExecutionMode(ctx, sdkprovider.CapabilityAction)

	output, err := p.ExecuteProvider(ctx, ProviderName, map[string]any{
		"command": "echo multi-writer",
	})
	require.NoError(t, err)
	data, ok := output.Data.(map[string]any)
	require.True(t, ok)

	// Output must appear in both places simultaneously.
	assert.Equal(t, "multi-writer\n", streamBuf.String(), "output must stream to ioStreams.Out")
	assert.Equal(t, "multi-writer\n", data["stdout"], "output must also be captured in data[stdout]")
	assert.True(t, output.Streamed)
}

// ---------------------------------------------------------------------------
// failOnError and streaming mode tests
// ---------------------------------------------------------------------------

func TestExecuteProvider_FailOnError_DefaultTrue_AllModes(t *testing.T) {
	p := NewPlugin()

	modes := []struct {
		name string
		ctx  context.Context
		raw  bool
	}{
		{
			name: "action mode",
			ctx:  sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityAction),
		},
		{
			name: "from mode",
			ctx:  sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityFrom),
		},
		{
			name: "from mode with raw true",
			ctx:  sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityFrom),
			raw:  true,
		},
		{
			name: "transform mode",
			ctx:  sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityTransform),
		},
		{
			name: "transform mode with raw true",
			ctx:  sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityTransform),
			raw:  true,
		},
		{
			name: "unset mode",
			ctx:  context.Background(),
		},
	}

	for _, tc := range modes {
		t.Run(tc.name, func(t *testing.T) {
			inputs := map[string]any{
				"command": "echo fail-msg >&2; exit 3",
			}
			if tc.raw {
				inputs["raw"] = true
			}

			output, err := p.ExecuteProvider(tc.ctx, ProviderName, inputs)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "command exited with code 3")
			assert.Contains(t, err.Error(), "fail-msg")
			require.NotNil(t, output)

			if tc.raw {
				_, isString := output.Data.(string)
				assert.True(t, isString, "raw mode should return Data as string")
				require.NotNil(t, output.Metadata)
				assert.Equal(t, 3, output.Metadata["exitCode"])
			} else {
				data, ok := output.Data.(map[string]any)
				require.True(t, ok)
				assert.Equal(t, 3, data["exitCode"])
				assert.Equal(t, false, data["success"])
			}
		})
	}
}

func TestExecuteProvider_FailOnError_ExplicitFalse_OptOut(t *testing.T) {
	p := NewPlugin()

	modes := []struct {
		name string
		ctx  context.Context
	}{
		{
			name: "action mode",
			ctx:  sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityAction),
		},
		{
			name: "from mode",
			ctx:  sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityFrom),
		},
		{
			name: "transform mode",
			ctx:  sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityTransform),
		},
		{
			name: "unset mode",
			ctx:  context.Background(),
		},
	}

	for _, tc := range modes {
		t.Run(tc.name, func(t *testing.T) {
			output, err := p.ExecuteProvider(tc.ctx, ProviderName, map[string]any{
				"command":     "echo partial-out; exit 5",
				"failOnError": false,
			})
			require.NoError(t, err)
			require.NotNil(t, output)

			data, ok := output.Data.(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "partial-out\n", data["stdout"])
			assert.Equal(t, 5, data["exitCode"])
			assert.Equal(t, false, data["success"])
		})
	}
}

func TestExecuteProvider_RawTrue_WithExplicitFailOnErrorFalse_Rejected(t *testing.T) {
	p := NewPlugin()

	tests := []struct {
		name       string
		ctx        context.Context
		wantErr    bool
		errContain string
	}{
		{
			name:       "from mode rejects raw true + failOnError false",
			ctx:        sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityFrom),
			wantErr:    true,
			errContain: "raw: true cannot be combined with failOnError: false in from mode",
		},
		{
			name:       "transform mode rejects raw true + failOnError false",
			ctx:        sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityTransform),
			wantErr:    true,
			errContain: "raw: true cannot be combined with failOnError: false in transform mode",
		},
		{
			name:    "action mode allows raw true + failOnError false because full map is returned",
			ctx:     sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityAction),
			wantErr: false,
		},
		{
			name:    "unset mode allows raw true + failOnError false because full map is returned",
			ctx:     context.Background(),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executed := false
			mockFn := shellexec.RunFunc(func(_ context.Context, _ *shellexec.RunOptions) (*shellexec.RunResult, error) {
				executed = true
				return &shellexec.RunResult{ExitCode: 0, Shell: shellexec.ShellAuto}, nil
			})
			ctx := shellexec.WithRunFunc(tt.ctx, mockFn)

			output, err := p.ExecuteProvider(ctx, ProviderName, map[string]any{
				"command":     "echo hello",
				"raw":         true,
				"failOnError": false,
			})
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContain)
				assert.Nil(t, output)
				assert.False(t, executed, "command must not execute when inputs are invalid")
			} else {
				require.NoError(t, err)
				require.NotNil(t, output)
				assert.True(t, executed)
			}
		})
	}
}

func TestExecuteProviderStream_RawTrue_WithExplicitFailOnErrorFalse_Rejected(t *testing.T) {
	p := NewPlugin()
	ctx := sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityFrom)

	var errChunk string
	err := p.ExecuteProviderStream(ctx, ProviderName, map[string]any{
		"command":     "echo hello",
		"raw":         true,
		"failOnError": false,
		"passthrough": true,
	}, func(c sdkplugin.StreamChunk) {
		if c.Error != "" {
			errChunk = c.Error
		}
	})
	require.NoError(t, err)
	assert.Contains(t, errChunk, "raw: true cannot be combined with failOnError: false in from mode")
}

func TestExecuteProvider_FailOnError_ActionMode_ZeroExitSucceeds(t *testing.T) {
	p := NewPlugin()
	ctx := sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityAction)

	output, err := p.ExecuteProvider(ctx, ProviderName, map[string]any{
		"command": "echo ok",
	})
	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 0, data["exitCode"])
	assert.Equal(t, true, data["success"])
}

func TestExecuteProvider_FailOnError_EmptyStderr(t *testing.T) {
	p := NewPlugin()
	ctx := sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityAction)

	_, err := p.ExecuteProvider(ctx, ProviderName, map[string]any{
		"command": "exit 1",
	})
	require.Error(t, err)
	// Error should mention exit code but not have a trailing colon for empty stderr.
	assert.Contains(t, err.Error(), "command exited with code 1")
	assert.NotContains(t, err.Error(), ": : ")
}

func TestDescribeWhatIf_FailOnError(t *testing.T) {
	p := NewPlugin()

	desc, err := p.DescribeWhatIf(context.Background(), ProviderName, map[string]any{
		"command":     "echo hello",
		"failOnError": true,
	})
	require.NoError(t, err)
	assert.Contains(t, desc, "will fail on non-zero exit code")

	desc, err = p.DescribeWhatIf(context.Background(), ProviderName, map[string]any{
		"command":     "echo hello",
		"failOnError": false,
	})
	require.NoError(t, err)
	assert.Contains(t, desc, "will NOT fail on non-zero exit code")
}

func TestFormatStderrSuffix(t *testing.T) {
	assert.Equal(t, "", formatStderrSuffix(""))
	assert.Equal(t, "", formatStderrSuffix("   "))
	assert.Equal(t, ": something broke", formatStderrSuffix("something broke"))
	assert.Equal(t, ": short msg", formatStderrSuffix("  short msg  "))

	longStderr := strings.Repeat("x", 600)
	result := formatStderrSuffix(longStderr)
	assert.True(t, strings.HasSuffix(result, "..."))
	assert.LessOrEqual(t, len(result), maxStderrInError+10) // ": " + content + "..."
}

func TestShouldFailOnError(t *testing.T) {
	tests := []struct {
		name     string
		inputs   map[string]any
		expected bool
	}{
		{
			name:     "omitted defaults true",
			inputs:   map[string]any{},
			expected: true,
		},
		{
			name:     "explicit true",
			inputs:   map[string]any{"failOnError": true},
			expected: true,
		},
		{
			name:     "explicit false",
			inputs:   map[string]any{"failOnError": false},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, shouldFailOnError(tt.inputs))
		})
	}
}

func TestShouldStream(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		inputs   map[string]any
		expected bool
	}{
		{
			name:     "action mode defaults true",
			ctx:      sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityAction),
			inputs:   map[string]any{},
			expected: true,
		},
		{
			name:     "action mode explicit passthrough false opts out",
			ctx:      sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityAction),
			inputs:   map[string]any{"passthrough": false},
			expected: false,
		},
		{
			name:     "action mode explicit passthrough true",
			ctx:      sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityAction),
			inputs:   map[string]any{"passthrough": true},
			expected: true,
		},
		{
			name:     "from mode defaults false",
			ctx:      sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityFrom),
			inputs:   map[string]any{},
			expected: false,
		},
		{
			name:     "from mode explicit passthrough true",
			ctx:      sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityFrom),
			inputs:   map[string]any{"passthrough": true},
			expected: true,
		},
		{
			name:     "transform mode defaults false",
			ctx:      sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityTransform),
			inputs:   map[string]any{},
			expected: false,
		},
		{
			name:     "transform mode explicit passthrough true",
			ctx:      sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityTransform),
			inputs:   map[string]any{"passthrough": true},
			expected: true,
		},
		{
			name:     "unset mode defaults false",
			ctx:      context.Background(),
			inputs:   map[string]any{},
			expected: false,
		},
		{
			name:     "unset mode explicit passthrough true",
			ctx:      context.Background(),
			inputs:   map[string]any{"passthrough": true},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, shouldStream(tt.ctx, tt.inputs))
		})
	}
}

func TestExecuteProviderStream_StreamingModes(t *testing.T) {
	p := NewPlugin()

	tests := []struct {
		name         string
		ctx          context.Context
		inputs       map[string]any
		wantStreamed bool
	}{
		{
			name:         "action mode streams live by default",
			ctx:          sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityAction),
			inputs:       map[string]any{"command": "printf 'out-line\n'; printf 'err-line\n' >&2"},
			wantStreamed: true,
		},
		{
			name:         "action mode with passthrough false buffers output",
			ctx:          sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityAction),
			inputs:       map[string]any{"command": "printf 'out-line\n'; printf 'err-line\n' >&2", "passthrough": false},
			wantStreamed: false,
		},
		{
			name:         "from mode buffers output by default",
			ctx:          sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityFrom),
			inputs:       map[string]any{"command": "printf 'out-line\n'; printf 'err-line\n' >&2"},
			wantStreamed: false,
		},
		{
			name:         "transform mode buffers output by default",
			ctx:          sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityTransform),
			inputs:       map[string]any{"command": "printf 'out-line\n'; printf 'err-line\n' >&2"},
			wantStreamed: false,
		},
		{
			name:         "unset mode buffers output by default",
			ctx:          context.Background(),
			inputs:       map[string]any{"command": "printf 'out-line\n'; printf 'err-line\n' >&2"},
			wantStreamed: false,
		},
		{
			name:         "from mode with passthrough true streams live",
			ctx:          sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityFrom),
			inputs:       map[string]any{"command": "printf 'out-line\n'; printf 'err-line\n' >&2", "passthrough": true},
			wantStreamed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdoutChunks, stderrChunks []byte
			var resultOut *sdkprovider.Output
			var errChunk string

			err := p.ExecuteProviderStream(tt.ctx, ProviderName, tt.inputs, func(c sdkplugin.StreamChunk) {
				stdoutChunks = append(stdoutChunks, c.Stdout...)
				stderrChunks = append(stderrChunks, c.Stderr...)
				if c.Result != nil {
					resultOut = c.Result
				}
				if c.Error != "" {
					errChunk = c.Error
				}
			})
			require.NoError(t, err)
			assert.Empty(t, errChunk)
			require.NotNil(t, resultOut)
			assert.Equal(t, tt.wantStreamed, resultOut.Streamed)

			// Both stdout and stderr must always be captured in Result.Data.
			data, ok := resultOut.Data.(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "out-line\n", data["stdout"])
			assert.Equal(t, "err-line\n", data["stderr"])

			if tt.wantStreamed {
				assert.Equal(t, "out-line\n", string(stdoutChunks))
				assert.Equal(t, "err-line\n", string(stderrChunks))
			} else {
				assert.Empty(t, stdoutChunks)
				assert.Empty(t, stderrChunks)
			}
		})
	}
}

func TestExecuteProviderStream_ActionMode_DryRun_DoesNotExecute(t *testing.T) {
	p := NewPlugin()

	executed := false
	mockFn := shellexec.RunFunc(func(_ context.Context, _ *shellexec.RunOptions) (*shellexec.RunResult, error) {
		executed = true
		return &shellexec.RunResult{ExitCode: 0, Shell: shellexec.ShellAuto}, nil
	})
	ctx := shellexec.WithRunFunc(context.Background(), mockFn)
	ctx = sdkprovider.WithExecutionMode(ctx, sdkprovider.CapabilityAction)
	ctx = sdkprovider.WithDryRun(ctx, true)

	var resultOut *sdkprovider.Output
	err := p.ExecuteProviderStream(ctx, ProviderName, map[string]any{
		"command": "echo should-not-run",
	}, func(c sdkplugin.StreamChunk) {
		if c.Result != nil {
			resultOut = c.Result
		}
	})
	require.NoError(t, err)
	assert.False(t, executed, "dry-run must not execute command in streaming action mode")
	require.NotNil(t, resultOut)
	assert.False(t, resultOut.Streamed)
	data, ok := resultOut.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, data["_dryRun"])
}

func TestIntegration_ActionMode_PassthroughFalse_BuffersOnly(t *testing.T) {
	p := NewPlugin()

	var streamBuf bytes.Buffer
	ctx := sdkprovider.WithIOStreams(context.Background(), &sdkprovider.IOStreams{
		Out: &streamBuf,
	})
	ctx = sdkprovider.WithExecutionMode(ctx, sdkprovider.CapabilityAction)

	output, err := p.ExecuteProvider(ctx, ProviderName, map[string]any{
		"command":     "echo buffered-only",
		"passthrough": false,
	})
	require.NoError(t, err)
	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Empty(t, streamBuf.String(), "passthrough: false must suppress live streaming in action mode")
	assert.Equal(t, "buffered-only\n", data["stdout"])
	assert.False(t, output.Streamed)
}

func TestExecuteProviderStream_FailOnError_ActionMode(t *testing.T) {
	p := NewPlugin()
	ctx := sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityAction)

	var stdoutData, stderrData []byte
	var gotError, gotResult bool
	var resultOut *sdkprovider.Output
	err := p.ExecuteProviderStream(ctx, ProviderName, map[string]any{
		"command": "echo visible-out; echo fail-msg >&2; exit 1",
	}, func(c sdkplugin.StreamChunk) {
		stdoutData = append(stdoutData, c.Stdout...)
		stderrData = append(stderrData, c.Stderr...)
		if c.Error != "" {
			gotError = true
			assert.Contains(t, c.Error, "command exited with code 1")
			assert.Contains(t, c.Error, "fail-msg")
		}
		if c.Result != nil {
			gotResult = true
			resultOut = c.Result
		}
	})
	require.NoError(t, err) // stream method returns nil; error goes via chunk
	assert.True(t, gotError, "expected an error chunk for non-zero exit in action mode")
	assert.True(t, gotResult, "expected a result chunk preserving output data")
	assert.Equal(t, "visible-out\n", string(stdoutData), "action stdout must stream live before failure")
	assert.Equal(t, "fail-msg\n", string(stderrData), "action stderr must stream live before failure")
	require.NotNil(t, resultOut)
	assert.True(t, resultOut.Streamed)
	data, ok := resultOut.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "visible-out\n", data["stdout"])
	assert.Equal(t, "fail-msg\n", data["stderr"])
}

func TestExecuteProviderStream_FailOnError_Buffered(t *testing.T) {
	p := NewPlugin()

	tests := []struct {
		name   string
		ctx    context.Context
		inputs map[string]any
	}{
		{
			name:   "from mode buffered failure",
			ctx:    sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityFrom),
			inputs: map[string]any{"command": "echo buffered-out; echo buffered-err >&2; exit 2"},
		},
		{
			name: "action mode with passthrough false buffered failure",
			ctx:  sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityAction),
			inputs: map[string]any{
				"command":     "echo buffered-out; echo buffered-err >&2; exit 2",
				"passthrough": false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var chunks []sdkplugin.StreamChunk
			err := p.ExecuteProviderStream(tt.ctx, ProviderName, tt.inputs, func(c sdkplugin.StreamChunk) {
				chunks = append(chunks, c)
			})
			require.NoError(t, err)

			// Buffered failure must emit exactly 2 chunks in order: Result first, then Error,
			// with zero live Stdout/Stderr chunks.
			require.Len(t, chunks, 2)
			require.NotNil(t, chunks[0].Result)
			assert.False(t, chunks[0].Result.Streamed)
			data, ok := chunks[0].Result.Data.(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "buffered-out\n", data["stdout"])
			assert.Equal(t, "buffered-err\n", data["stderr"])
			assert.Equal(t, 2, data["exitCode"])

			assert.Contains(t, chunks[1].Error, "command exited with code 2")
			assert.Contains(t, chunks[1].Error, "buffered-err")
		})
	}
}

func TestExecuteProviderStream_Echo_StreamsToStderrChunkWithoutPollutingResult(t *testing.T) {
	p := NewPlugin()
	ctx := sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityAction)

	var stdoutData, stderrData []byte
	var resultOut *sdkprovider.Output
	err := p.ExecuteProviderStream(ctx, ProviderName, map[string]any{
		"command": "echo hello",
		"echo":    true,
	}, func(c sdkplugin.StreamChunk) {
		stdoutData = append(stdoutData, c.Stdout...)
		stderrData = append(stderrData, c.Stderr...)
		if c.Result != nil {
			resultOut = c.Result
		}
	})
	require.NoError(t, err)
	assert.Equal(t, "hello\n", string(stdoutData))
	assert.Contains(t, string(stderrData), "> echo hello\n")
	require.NotNil(t, resultOut)
	assert.True(t, resultOut.Streamed)
	data, ok := resultOut.Data.(map[string]any)
	require.True(t, ok)
	assert.Empty(t, data["stderr"], "echo prefix must not pollute captured stderr")
}

func TestExecuteProviderStream_ActionMode_FailOnErrorFalse_NoErrorChunk(t *testing.T) {
	p := NewPlugin()
	ctx := sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityAction)

	var stdoutData []byte
	var errChunk string
	var resultOut *sdkprovider.Output
	err := p.ExecuteProviderStream(ctx, ProviderName, map[string]any{
		"command":     "echo partial; exit 7",
		"failOnError": false,
	}, func(c sdkplugin.StreamChunk) {
		stdoutData = append(stdoutData, c.Stdout...)
		if c.Error != "" {
			errChunk = c.Error
		}
		if c.Result != nil {
			resultOut = c.Result
		}
	})
	require.NoError(t, err)
	assert.Empty(t, errChunk, "failOnError: false must suppress error chunk")
	assert.Equal(t, "partial\n", string(stdoutData))
	require.NotNil(t, resultOut)
	assert.True(t, resultOut.Streamed)
	data, ok := resultOut.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 7, data["exitCode"])
	assert.Equal(t, false, data["success"])
}

func TestExecuteProviderStream_FailOnError_Passthrough_ActionMode(t *testing.T) {
	p := NewPlugin()
	ctx := sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityAction)

	var chunks []sdkplugin.StreamChunk
	err := p.ExecuteProviderStream(ctx, ProviderName, map[string]any{
		"command":     "echo pt-fail >&2; exit 2",
		"passthrough": true,
	}, func(c sdkplugin.StreamChunk) {
		chunks = append(chunks, c)
	})
	require.NoError(t, err)

	var gotError, gotResult bool
	for _, c := range chunks {
		if c.Error != "" {
			gotError = true
			assert.Contains(t, c.Error, "command exited with code 2")
			assert.Contains(t, c.Error, "pt-fail", "passthrough stderr should be captured in error message")
		}
		if c.Result != nil {
			gotResult = true
		}
	}
	assert.True(t, gotError, "expected an error chunk for passthrough + failOnError")
	assert.True(t, gotResult, "expected a result chunk preserving output data")
}

func TestExtractExitInfo(t *testing.T) {
	tests := []struct {
		name       string
		output     *sdkprovider.Output
		wantCode   int
		wantStderr string
	}{
		{
			name:     "nil output",
			output:   nil,
			wantCode: 0,
		},
		{
			name:       "map data with exit code",
			output:     &sdkprovider.Output{Data: map[string]any{"exitCode": 42, "stderr": "boom"}},
			wantCode:   42,
			wantStderr: "boom",
		},
		{
			name:       "string data with metadata fallback",
			output:     &sdkprovider.Output{Data: "raw output", Metadata: map[string]any{"exitCode": 7, "stderr": "meta-err"}},
			wantCode:   7,
			wantStderr: "meta-err",
		},
		{
			name:     "string data without metadata",
			output:   &sdkprovider.Output{Data: "raw output"},
			wantCode: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stderr := extractExitInfo(tt.output)
			assert.Equal(t, tt.wantCode, code)
			assert.Equal(t, tt.wantStderr, stderr)
		})
	}
}

type shortOrErrWriter struct {
	n   int
	err error
}

func (w shortOrErrWriter) Write(_ []byte) (int, error) {
	return w.n, w.err
}

func TestCaptureWriter_ForwardErrorsAndStopForward(t *testing.T) {
	t.Run("propagates forward write error", func(t *testing.T) {
		wantErr := errors.New("broken stream")
		cw := &captureWriter{forward: shortOrErrWriter{n: 2, err: wantErr}}
		n, err := cw.Write([]byte("hello"))
		require.ErrorIs(t, err, wantErr)
		assert.Equal(t, 2, n)
		assert.Equal(t, "hello", cw.String())
	})

	t.Run("returns ErrShortWrite on truncated forward write", func(t *testing.T) {
		cw := &captureWriter{forward: shortOrErrWriter{n: 2}}
		n, err := cw.Write([]byte("hello"))
		require.ErrorIs(t, err, io.ErrShortWrite)
		assert.Equal(t, 2, n)
		assert.Equal(t, "hello", cw.String())
	})

	t.Run("StopForward detaches live stream while keeping capture working", func(t *testing.T) {
		var live bytes.Buffer
		cw := &captureWriter{forward: &live}
		_, err := cw.Write([]byte("first\n"))
		require.NoError(t, err)
		cw.StopForward()
		_, err = cw.Write([]byte("second\n"))
		require.NoError(t, err)
		assert.Equal(t, "first\n", live.String())
		assert.Equal(t, "first\nsecond\n", cw.String())
	})

	t.Run("chunkWriter suppresses writes after closed", func(t *testing.T) {
		closed := false
		var got []byte
		w := &chunkWriter{
			cb:       func(c sdkplugin.StreamChunk) { got = append(got, c.Stdout...) },
			isStdout: true,
			closed:   &closed,
		}
		_, err := w.Write([]byte("before\n"))
		require.NoError(t, err)
		closed = true
		n, err := w.Write([]byte("after\n"))
		require.NoError(t, err)
		assert.Equal(t, len("after\n"), n)
		assert.Equal(t, "before\n", string(got))
	})
}

func TestParallelSubshellJobs_NoDataRace(t *testing.T) {
	p := NewPlugin()
	// mvdan.cc/sh's echo builtin emits its argument and trailing newline in two
	// separate Write calls, while printf emits each formatted string in a single
	// Write call. Exercise both: echo verifies race-free capture/stream parity on
	// "echo one & echo two & wait", and printf verifies atomic single-Write lines.
	const parallelEchoCmd = "echo one & echo two & echo err-one >&2 & echo err-two >&2 & wait"
	const parallelPrintfCmd = `printf 'one\n' & printf 'two\n' & printf 'err-one\n' >&2 & printf 'err-two\n' >&2 & wait`
	wantStdout := []string{"one", "two"}
	wantStderr := []string{"err-one", "err-two"}

	splitLines := func(s string) []string {
		trimmed := strings.TrimSuffix(s, "\n")
		if trimmed == "" {
			return nil
		}
		return strings.Split(trimmed, "\n")
	}

	assertEchoTokens := func(t *testing.T, out, errStr string) {
		t.Helper()
		assert.Contains(t, out, "one")
		assert.Contains(t, out, "two")
		assert.Equal(t, 2, strings.Count(out, "\n"))
		assert.Len(t, out, len("one\ntwo\n"))

		assert.Contains(t, errStr, "err-one")
		assert.Contains(t, errStr, "err-two")
		assert.Equal(t, 2, strings.Count(errStr, "\n"))
		assert.Len(t, errStr, len("err-one\nerr-two\n"))
	}

	t.Run("ExecuteProviderStream in CapabilityAction", func(t *testing.T) {
		ctx := sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityAction)

		for range 10 {
			var echoOutChunks, echoErrChunks []byte
			var echoResult *sdkprovider.Output
			err := p.ExecuteProviderStream(ctx, ProviderName, map[string]any{
				"command": parallelEchoCmd,
			}, func(c sdkplugin.StreamChunk) {
				echoOutChunks = append(echoOutChunks, c.Stdout...)
				echoErrChunks = append(echoErrChunks, c.Stderr...)
				if c.Result != nil {
					echoResult = c.Result
				}
				assert.Empty(t, c.Error)
			})
			require.NoError(t, err)
			require.NotNil(t, echoResult)
			assert.True(t, echoResult.Streamed)
			echoData := echoResult.Data.(map[string]any)
			assertEchoTokens(t, echoData["stdout"].(string), echoData["stderr"].(string))
			assert.Equal(t, echoData["stdout"].(string), string(echoOutChunks))
			assert.Equal(t, echoData["stderr"].(string), string(echoErrChunks))

			var stdoutChunks, stderrChunks []byte
			var resultOut *sdkprovider.Output
			var errChunk string

			err = p.ExecuteProviderStream(ctx, ProviderName, map[string]any{
				"command": parallelPrintfCmd,
			}, func(c sdkplugin.StreamChunk) {
				stdoutChunks = append(stdoutChunks, c.Stdout...)
				stderrChunks = append(stderrChunks, c.Stderr...)
				if c.Result != nil {
					resultOut = c.Result
				}
				if c.Error != "" {
					errChunk = c.Error
				}
			})
			require.NoError(t, err)
			assert.Empty(t, errChunk)
			require.NotNil(t, resultOut)
			assert.True(t, resultOut.Streamed)

			data, ok := resultOut.Data.(map[string]any)
			require.True(t, ok)

			capturedOut, ok := data["stdout"].(string)
			require.True(t, ok)
			capturedErr, ok := data["stderr"].(string)
			require.True(t, ok)

			assert.ElementsMatch(t, wantStdout, splitLines(string(stdoutChunks)))
			assert.ElementsMatch(t, wantStderr, splitLines(string(stderrChunks)))
			assert.ElementsMatch(t, wantStdout, splitLines(capturedOut))
			assert.ElementsMatch(t, wantStderr, splitLines(capturedErr))
			assert.Equal(t, capturedOut, string(stdoutChunks))
			assert.Equal(t, capturedErr, string(stderrChunks))
		}
	})

	t.Run("ExecuteProvider with IOStreams in CapabilityAction", func(t *testing.T) {
		for range 10 {
			var echoStreamOut, echoStreamErr bytes.Buffer
			echoCtx := sdkprovider.WithIOStreams(context.Background(), &sdkprovider.IOStreams{
				Out:    &echoStreamOut,
				ErrOut: &echoStreamErr,
			})
			echoCtx = sdkprovider.WithExecutionMode(echoCtx, sdkprovider.CapabilityAction)
			echoOutput, err := p.ExecuteProvider(echoCtx, ProviderName, map[string]any{
				"command": parallelEchoCmd,
			})
			require.NoError(t, err)
			require.NotNil(t, echoOutput)
			assert.True(t, echoOutput.Streamed)
			echoData := echoOutput.Data.(map[string]any)
			assertEchoTokens(t, echoData["stdout"].(string), echoData["stderr"].(string))
			assert.Equal(t, echoData["stdout"].(string), echoStreamOut.String())
			assert.Equal(t, echoData["stderr"].(string), echoStreamErr.String())

			var streamOut, streamErr bytes.Buffer
			ctx := sdkprovider.WithIOStreams(context.Background(), &sdkprovider.IOStreams{
				Out:    &streamOut,
				ErrOut: &streamErr,
			})
			ctx = sdkprovider.WithExecutionMode(ctx, sdkprovider.CapabilityAction)

			output, err := p.ExecuteProvider(ctx, ProviderName, map[string]any{
				"command": parallelPrintfCmd,
				"echo":    true,
			})
			require.NoError(t, err)
			require.NotNil(t, output)
			assert.True(t, output.Streamed)

			data, ok := output.Data.(map[string]any)
			require.True(t, ok)

			capturedOut, ok := data["stdout"].(string)
			require.True(t, ok)
			capturedErr, ok := data["stderr"].(string)
			require.True(t, ok)

			assert.ElementsMatch(t, wantStdout, splitLines(streamOut.String()))
			assert.ElementsMatch(t, wantStdout, splitLines(capturedOut))
			assert.Equal(t, capturedOut, streamOut.String())

			assert.ElementsMatch(t, wantStderr, splitLines(capturedErr))
			streamErrLines := splitLines(streamErr.String())
			require.Len(t, streamErrLines, 3)
			assert.Equal(t, "> "+parallelPrintfCmd, streamErrLines[0])
			assert.ElementsMatch(t, wantStderr, streamErrLines[1:])
		}
	})

	t.Run("ExecuteProvider with shared IOStreams writer in CapabilityAction", func(t *testing.T) {
		for range 10 {
			var combined bytes.Buffer
			ctx := sdkprovider.WithIOStreams(context.Background(), &sdkprovider.IOStreams{
				Out:    &combined,
				ErrOut: &combined,
			})
			ctx = sdkprovider.WithExecutionMode(ctx, sdkprovider.CapabilityAction)

			output, err := p.ExecuteProvider(ctx, ProviderName, map[string]any{
				"command": parallelPrintfCmd,
			})
			require.NoError(t, err)
			require.NotNil(t, output)
			assert.True(t, output.Streamed)

			data, ok := output.Data.(map[string]any)
			require.True(t, ok)
			assert.ElementsMatch(t, wantStdout, splitLines(data["stdout"].(string)))
			assert.ElementsMatch(t, wantStderr, splitLines(data["stderr"].(string)))
			assert.ElementsMatch(t, append(append([]string{}, wantStdout...), wantStderr...), splitLines(combined.String()))
		}
	})

	t.Run("ExecuteProvider buffered in CapabilityFrom", func(t *testing.T) {
		ctx := sdkprovider.WithExecutionMode(context.Background(), sdkprovider.CapabilityFrom)

		for range 10 {
			output, err := p.ExecuteProvider(ctx, ProviderName, map[string]any{
				"command": parallelPrintfCmd,
			})
			require.NoError(t, err)
			require.NotNil(t, output)
			assert.False(t, output.Streamed)

			data, ok := output.Data.(map[string]any)
			require.True(t, ok)
			assert.ElementsMatch(t, wantStdout, splitLines(data["stdout"].(string)))
			assert.ElementsMatch(t, wantStderr, splitLines(data["stderr"].(string)))
		}
	})
}

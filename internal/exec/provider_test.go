// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package exec

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
	sdkprovider "github.com/oakwood-commons/scafctl-plugin-sdk/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	assert.Equal(t, ProviderName, d.Name)
	assert.Equal(t, "Exec Provider", d.DisplayName)
	assert.Equal(t, "v1", d.APIVersion)
	assert.NotNil(t, d.Schema)
	assert.Contains(t, d.Capabilities, sdkprovider.CapabilityAction)
	assert.Contains(t, d.Capabilities, sdkprovider.CapabilityFrom)
	assert.Contains(t, d.Capabilities, sdkprovider.CapabilityTransform)
	assert.Contains(t, d.Schema.Required, "command")
	assert.NotEmpty(t, d.Examples)
	assert.NotNil(t, d.OutputSchemas[sdkprovider.CapabilityAction])
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
	require.NoError(t, err)

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

	if err != nil {
		t.Logf("Got error as expected: %v", err)
	} else {
		require.NotNil(t, output)
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
	require.NoError(t, err)
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

	if err != nil {
		errStr := err.Error()
		assert.True(t, strings.Contains(errStr, "context") || strings.Contains(errStr, "signal"),
			"Expected context or signal error, got: %s", errStr)
	} else {
		require.NotNil(t, output)
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
	assert.Empty(t, data["stdout"], "passthrough should not capture stdout")
	assert.Equal(t, 0, data["exitCode"])
	assert.Equal(t, true, data["success"])

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

func TestExecuteProviderStream_NotSupported(t *testing.T) {
	p := NewPlugin()
	err := p.ExecuteProviderStream(context.Background(), ProviderName, nil, nil)
	assert.ErrorIs(t, err, sdkplugin.ErrStreamingNotSupported)
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

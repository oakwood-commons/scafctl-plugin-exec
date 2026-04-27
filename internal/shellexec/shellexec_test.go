// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package shellexec

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skipIfNotFound skips the test when a binary is not in PATH.
func skipIfNotFound(t *testing.T, binary string) {
	t.Helper()
	if _, err := exec.LookPath(binary); err != nil {
		t.Skipf("%s not found in PATH", binary)
	}
}

// ---- ValidShellTypes --------------------------------------------------------

func TestValidShellTypes(t *testing.T) {
	types := ValidShellTypes()
	assert.ElementsMatch(t, []string{"auto", "sh", "bash", "pwsh", "cmd"}, types)
}

// ---- ShellType.IsValid ------------------------------------------------------

func TestShellType_IsValid(t *testing.T) {
	tests := []struct {
		name  string
		shell ShellType
		want  bool
	}{
		{"auto", ShellAuto, true},
		{"sh", ShellSh, true},
		{"bash", ShellBash, true},
		{"pwsh", ShellPwsh, true},
		{"cmd", ShellCmd, true},
		{"empty", ShellType(""), false},
		{"unknown", ShellType("zsh"), false},
		{"uppercase", ShellType("BASH"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.shell.IsValid())
		})
	}
}

// ---- BuildFullCommand -------------------------------------------------------

func TestBuildFullCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		args    []string
		want    string
	}{
		{
			name:    "no args",
			command: "echo",
			args:    nil,
			want:    "echo",
		},
		{
			name:    "empty args slice",
			command: "echo",
			args:    []string{},
			want:    "echo",
		},
		{
			name:    "single arg",
			command: "echo",
			args:    []string{"hello"},
			want:    "echo 'hello'",
		},
		{
			name:    "multiple args",
			command: "echo",
			args:    []string{"hello", "world"},
			want:    "echo 'hello' 'world'",
		},
		{
			name:    "arg with spaces",
			command: "echo",
			args:    []string{"hello world"},
			want:    "echo 'hello world'",
		},
		{
			name:    "arg with single quote",
			command: "echo",
			args:    []string{"it's"},
			want:    "echo 'it'\\''s'",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, BuildFullCommand(tc.command, tc.args))
		})
	}
}

// ---- ShellQuote -------------------------------------------------------------

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain word", "hello", "'hello'"},
		{"empty string", "", "''"},
		{"spaces", "hello world", "'hello world'"},
		{"single quote", "it's", "'it'\\''s'"},
		{"multiple single quotes", "a'b'c", "'a'\\''b'\\''c'"},
		{"dollar sign", "$HOME", "'$HOME'"},
		{"backtick", "`date`", "'`date`'"},
		{"newline", "a\nb", "'a\nb'"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ShellQuote(tc.input))
		})
	}
}

// ---- MergeEnv ---------------------------------------------------------------

func TestMergeEnv(t *testing.T) {
	extra := map[string]any{
		"SCAFCTL_MERGEENV_STR": "testvalue",
		"SCAFCTL_MERGEENV_INT": 42,
	}
	env := MergeEnv(extra)

	// Must contain OS environment
	osEnv := os.Environ()
	assert.GreaterOrEqual(t, len(env), len(osEnv))

	var foundStr, foundInt bool
	for _, e := range env {
		if e == "SCAFCTL_MERGEENV_STR=testvalue" {
			foundStr = true
		}
		if e == "SCAFCTL_MERGEENV_INT=42" {
			foundInt = true
		}
	}
	assert.True(t, foundStr, "expected SCAFCTL_MERGEENV_STR=testvalue in env")
	assert.True(t, foundInt, "expected SCAFCTL_MERGEENV_INT=42 in env")
}

// ---- Run: validation --------------------------------------------------------

func TestRun_NilOptions(t *testing.T) {
	_, err := Run(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil options")
}

func TestRun_EmptyCommand(t *testing.T) {
	_, err := Run(context.Background(), &RunOptions{Command: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty command")
}

func TestRun_InvalidShell(t *testing.T) {
	_, err := Run(context.Background(), &RunOptions{
		Command: "echo hello",
		Shell:   ShellType("zsh"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported shell type")
	assert.Contains(t, err.Error(), "zsh")
}

func TestRun_CmdShellOnNonWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test only applies to non-Windows")
	}
	_, err := Run(context.Background(), &RunOptions{
		Command: "echo hello",
		Shell:   ShellCmd,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only available on Windows")
}

// ---- Run: embedded shell (auto / sh) ----------------------------------------

func TestRun_Embedded_Echo(t *testing.T) {
	var out bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "echo",
		Args:    []string{"hello", "world"},
		Shell:   ShellAuto,
		Stdout:  &out,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, ShellAuto, result.Shell)
	assert.Equal(t, "hello world\n", out.String())
}

func TestRun_Embedded_ShAlias(t *testing.T) {
	var out bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "echo 'sh-alias'",
		Shell:   ShellSh,
		Stdout:  &out,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, ShellSh, result.Shell)
	assert.Equal(t, "sh-alias\n", out.String())
}

func TestRun_Embedded_DefaultShell(t *testing.T) {
	// Shell field empty — should default to ShellAuto
	var out bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "echo default",
		Stdout:  &out,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "default\n", out.String())
}

func TestRun_Embedded_NonZeroExitCode(t *testing.T) {
	result, err := Run(context.Background(), &RunOptions{
		Command: "exit 3",
		Shell:   ShellAuto,
	})
	require.NoError(t, err)
	assert.Equal(t, 3, result.ExitCode)
}

func TestRun_Embedded_Stderr(t *testing.T) {
	var errBuf bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "echo error >&2",
		Shell:   ShellAuto,
		Stderr:  &errBuf,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "error\n", errBuf.String())
}

func TestRun_Embedded_Stdin(t *testing.T) {
	// cat is provided by coreutils on Windows; skip if coreutils are disabled
	if runtime.GOOS == "windows" && !useCoreUtils {
		t.Skip("cat requires coreutils on Windows (SCAFCTL_CORE_UTILS=0)")
	}
	var out bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "cat",
		Shell:   ShellAuto,
		Stdin:   strings.NewReader("stdin-data\n"),
		Stdout:  &out,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "stdin-data\n", out.String())
}

func TestRun_Embedded_WorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "pwd",
		Shell:   ShellAuto,
		Dir:     dir,
		Stdout:  &out,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	// Canonicalize both paths to handle symlinks (e.g., /var -> /private/var on macOS)
	wantDir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	wantDir = filepath.Clean(wantDir)
	gotRaw := strings.TrimSpace(out.String())
	gotDir, err := filepath.EvalSymlinks(gotRaw)
	require.NoError(t, err)
	gotDir = filepath.Clean(gotDir)
	assert.Equal(t, wantDir, gotDir)
}

func TestRun_Embedded_CustomEnv(t *testing.T) {
	var out bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "echo $SCAFCTL_TEST_VAR",
		Shell:   ShellAuto,
		Env:     []string{"SCAFCTL_TEST_VAR=custom-value"},
		Stdout:  &out,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "custom-value\n", out.String())
}

func TestRun_Embedded_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := Run(ctx, &RunOptions{
		Command: "echo hello",
		Shell:   ShellAuto,
	})
	// Cancelled context should propagate as an error
	require.Error(t, err)
}

func TestRun_Embedded_DevNull(t *testing.T) {
	// Ensure /dev/null redirect works cross-platform via openHandler
	var out bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "echo visible; echo hidden >/dev/null",
		Shell:   ShellAuto,
		Stdout:  &out,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "visible\n", out.String())
}

// ---- Run: bash (external) ---------------------------------------------------

func TestRun_Bash_Echo(t *testing.T) {
	skipIfNotFound(t, "bash")
	var out bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "echo",
		Args:    []string{"bash-hello"},
		Shell:   ShellBash,
		Stdout:  &out,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, ShellType("bash"), result.Shell)
	assert.Equal(t, "bash-hello\n", out.String())
}

func TestRun_Bash_NonZeroExitCode(t *testing.T) {
	skipIfNotFound(t, "bash")
	result, err := Run(context.Background(), &RunOptions{
		Command: "exit 7",
		Shell:   ShellBash,
	})
	require.NoError(t, err)
	assert.Equal(t, 7, result.ExitCode)
}

func TestRun_Bash_WorkingDirectory(t *testing.T) {
	skipIfNotFound(t, "bash")
	// MSYS/Git Bash on Windows prints POSIX-style paths (e.g. /c/Users/...)
	// which filepath.EvalSymlinks cannot resolve as native Windows paths.
	if runtime.GOOS == "windows" {
		t.Skip("bash pwd output is POSIX-style on Windows; path canonicalization not supported")
	}
	dir := t.TempDir()
	var out bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "pwd",
		Shell:   ShellBash,
		Dir:     dir,
		Stdout:  &out,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	// Canonicalize both paths to handle symlinks (e.g., /var -> /private/var on macOS)
	wantDir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	wantDir = filepath.Clean(wantDir)
	gotRaw := strings.TrimSpace(out.String())
	gotDir, err := filepath.EvalSymlinks(gotRaw)
	require.NoError(t, err)
	gotDir = filepath.Clean(gotDir)
	assert.Equal(t, wantDir, gotDir)
}

func TestRun_Bash_CustomEnv(t *testing.T) {
	skipIfNotFound(t, "bash")
	var out bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "echo $SCAFCTL_TEST_BASH",
		Shell:   ShellBash,
		Env:     MergeEnv(map[string]any{"SCAFCTL_TEST_BASH": "bash-env"}),
		Stdout:  &out,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "bash-env\n", out.String())
}

// ---- RunSimple --------------------------------------------------------------

func TestRunSimple_Success(t *testing.T) {
	stdout, stderr, exitCode, err := RunSimple(context.Background(), ShellAuto, "echo", []string{"simple"})
	require.NoError(t, err)
	assert.Equal(t, "simple\n", stdout)
	assert.Equal(t, "", stderr)
	assert.Equal(t, 0, exitCode)
}

func TestRunSimple_NonZeroExit(t *testing.T) {
	_, _, exitCode, err := RunSimple(context.Background(), ShellAuto, "exit 2", nil)
	require.NoError(t, err)
	assert.Equal(t, 2, exitCode)
}

func TestRunSimple_Error(t *testing.T) {
	_, _, exitCode, err := RunSimple(context.Background(), ShellAuto, "", nil)
	require.Error(t, err)
	assert.Equal(t, -1, exitCode)
}

func TestRunSimple_StderrCapture(t *testing.T) {
	stdout, stderr, exitCode, err := RunSimple(context.Background(), ShellAuto, "echo err >&2", nil)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, "", stdout)
	assert.Equal(t, "err\n", stderr)
}

// ---- RunWithContext / mock injection ----------------------------------------

func TestRunWithContext_UsesMock(t *testing.T) {
	called := false
	mockFn := func(_ context.Context, _ *RunOptions) (*RunResult, error) {
		called = true
		return &RunResult{ExitCode: 42, Shell: ShellAuto}, nil
	}

	ctx := WithRunFunc(context.Background(), mockFn)
	result, err := RunWithContext(ctx, &RunOptions{Command: "echo hello"})
	require.NoError(t, err)
	assert.True(t, called, "mock function should have been called")
	assert.Equal(t, 42, result.ExitCode)
}

func TestRunWithContext_FallsBackToReal(t *testing.T) {
	var out bytes.Buffer
	result, err := RunWithContext(context.Background(), &RunOptions{
		Command: "echo real",
		Shell:   ShellAuto,
		Stdout:  &out,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "real\n", out.String())
}

func TestRunFuncFromContext_Missing(t *testing.T) {
	fn, ok := RunFuncFromContext(context.Background())
	assert.False(t, ok)
	assert.Nil(t, fn)
}

func TestRunFuncFromContext_Present(t *testing.T) {
	mockFn := func(_ context.Context, _ *RunOptions) (*RunResult, error) {
		return nil, nil
	}
	ctx := WithRunFunc(context.Background(), mockFn)
	fn, ok := RunFuncFromContext(ctx)
	assert.True(t, ok)
	assert.NotNil(t, fn)
}

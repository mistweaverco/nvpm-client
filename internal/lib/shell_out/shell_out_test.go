package shell_out

import (
	"io"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShellOut(t *testing.T) {
	t.Run("shell out with echo command", func(t *testing.T) {
		// Test with a simple echo command that should work on most systems
		exitCode, err := ShellOut("echo", []string{"hello"}, "", nil)
		assert.NoError(t, err)
		assert.Equal(t, 0, exitCode)
	})

	t.Run("shell out with command that exits non-zero", func(t *testing.T) {
		exitCode, err := ShellOut("false", []string{}, "", nil)
		assert.Error(t, err)
		assert.Equal(t, 1, exitCode)
	})

	t.Run("shell out with invalid command", func(t *testing.T) {
		// Test with a command that doesn't exist
		exitCode, err := ShellOut("nonexistentcommand12345", []string{}, "", nil)
		assert.Error(t, err)
		assert.Equal(t, -1, exitCode)
	})

	t.Run("shell out with custom directory", func(t *testing.T) {
		// Test with current directory
		currentDir, _ := os.Getwd()
		exitCode, shellErr := ShellOut("pwd", []string{}, currentDir, nil)
		assert.NoError(t, shellErr)
		assert.Equal(t, 0, exitCode)
	})

	t.Run("shell out with custom environment", func(t *testing.T) {
		// Test with custom environment variable
		customEnv := []string{"CUSTOM_VAR=test_value"}
		exitCode, _ := ShellOut("echo", []string{"$CUSTOM_VAR"}, "", customEnv)
		// Note: This might not work as expected on all systems due to shell interpretation
		// But we can at least test that it doesn't panic
		assert.NotEqual(t, -1, exitCode) // Should not be the error exit code
	})

	t.Run("shell out discards chatty command output", func(t *testing.T) {
		// Dup the process stdout/stderr FDs onto pipes so inherited child I/O
		// would be visible if ShellOut failed to discard it.
		rOut, wOut, err := os.Pipe()
		assert.NoError(t, err)
		rErr, wErr, err := os.Pipe()
		assert.NoError(t, err)

		savedOut, err := syscall.Dup(int(os.Stdout.Fd()))
		assert.NoError(t, err)
		savedErr, err := syscall.Dup(int(os.Stderr.Fd()))
		assert.NoError(t, err)

		assert.NoError(t, syscall.Dup2(int(wOut.Fd()), int(os.Stdout.Fd())))
		assert.NoError(t, syscall.Dup2(int(wErr.Fd()), int(os.Stderr.Fd())))

		exitCode, runErr := ShellOut("sh", []string{"-c", "echo leaked-out; echo leaked-err >&2"}, "", nil)

		assert.NoError(t, syscall.Dup2(savedOut, int(os.Stdout.Fd())))
		assert.NoError(t, syscall.Dup2(savedErr, int(os.Stderr.Fd())))
		_ = syscall.Close(savedOut)
		_ = syscall.Close(savedErr)
		_ = wOut.Close()
		_ = wErr.Close()

		assert.NoError(t, runErr)
		assert.Equal(t, 0, exitCode)

		outBytes, _ := io.ReadAll(rOut)
		errBytes, _ := io.ReadAll(rErr)
		_ = rOut.Close()
		_ = rErr.Close()
		assert.NotContains(t, string(outBytes), "leaked-out")
		assert.NotContains(t, string(errBytes), "leaked-err")
	})
}

func TestHasCommand(t *testing.T) {
	t.Run("has command with echo", func(t *testing.T) {
		// echo should exist on most systems
		exists := HasCommand("echo", []string{}, nil)
		assert.True(t, exists)
	})

	t.Run("has command returns false on exit error", func(t *testing.T) {
		exists := HasCommand("false", []string{}, nil)
		assert.False(t, exists)
	})

	t.Run("has command with custom env and args", func(t *testing.T) {
		exists := HasCommand("sh", []string{"-c", "exit 0"}, []string{"CUSTOM_VAR=test"})
		assert.True(t, exists)
	})

	t.Run("has command with nonexistent command", func(t *testing.T) {
		// This command should not exist
		exists := HasCommand("nonexistentcommand12345", []string{}, nil)
		assert.False(t, exists)
	})

	t.Run("has command discards chatty command output", func(t *testing.T) {
		rOut, wOut, err := os.Pipe()
		assert.NoError(t, err)
		rErr, wErr, err := os.Pipe()
		assert.NoError(t, err)

		savedOut, err := syscall.Dup(int(os.Stdout.Fd()))
		assert.NoError(t, err)
		savedErr, err := syscall.Dup(int(os.Stderr.Fd()))
		assert.NoError(t, err)

		assert.NoError(t, syscall.Dup2(int(wOut.Fd()), int(os.Stdout.Fd())))
		assert.NoError(t, syscall.Dup2(int(wErr.Fd()), int(os.Stderr.Fd())))

		exists := HasCommand("sh", []string{"-c", "echo leaked-out; echo leaked-err >&2; exit 0"}, nil)

		assert.NoError(t, syscall.Dup2(savedOut, int(os.Stdout.Fd())))
		assert.NoError(t, syscall.Dup2(savedErr, int(os.Stderr.Fd())))
		_ = syscall.Close(savedOut)
		_ = syscall.Close(savedErr)
		_ = wOut.Close()
		_ = wErr.Close()

		assert.True(t, exists)

		outBytes, _ := io.ReadAll(rOut)
		errBytes, _ := io.ReadAll(rErr)
		_ = rOut.Close()
		_ = rErr.Close()
		assert.NotContains(t, string(outBytes), "leaked-out")
		assert.NotContains(t, string(errBytes), "leaked-err")
	})
}

func TestShellOutCapture(t *testing.T) {
	t.Run("capture echo output", func(t *testing.T) {
		// Test capturing output from echo
		exitCode, output, err := ShellOutCapture("echo", []string{"hello world"}, "", nil)
		assert.NoError(t, err)
		assert.Equal(t, 0, exitCode)
		assert.Contains(t, output, "hello world")
	})

	t.Run("capture command with error", func(t *testing.T) {
		// Test capturing output from a command that fails
		exitCode, output, err := ShellOutCapture("nonexistentcommand12345", []string{}, "", nil)
		assert.Error(t, err)
		assert.Equal(t, -1, exitCode)
		assert.Empty(t, output)
	})

	t.Run("capture exit error with output and code", func(t *testing.T) {
		exitCode, output, err := ShellOutCapture("sh", []string{"-c", "echo oops; exit 2"}, "", nil)
		assert.Error(t, err)
		assert.Equal(t, 2, exitCode)
		assert.Contains(t, output, "oops")
	})

	// Cover env merging path
	t.Run("capture with custom env merged", func(t *testing.T) {
		exitCode, output, err := ShellOutCapture("sh", []string{"-c", "echo $MY_VAR"}, "", []string{"MY_VAR=xyz"})
		assert.NoError(t, err)
		assert.Equal(t, 0, exitCode)
		assert.Contains(t, output, "xyz")
	})
}

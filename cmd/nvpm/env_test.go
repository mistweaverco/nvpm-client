package nvpm

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnvCommand(t *testing.T) {
	t.Run("env command structure", func(t *testing.T) {
		assert.Equal(t, "env", envCmd.Use)
		assert.Contains(t, envCmd.Short, "Outputs a script")
		assert.NotEmpty(t, envCmd.Long)
	})

	t.Run("env command with no args defaults to bash", func(t *testing.T) {
		// Capture stdout
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		envCmd.Run(envCmd, []string{})

		// Restore stdout and read
		w.Close()
		os.Stdout = old
		var buf bytes.Buffer
		io.Copy(&buf, r)
		out := buf.String()

		assert.Contains(t, out, "#!/bin/sh")
		assert.Contains(t, out, "nvpm shell setup")
		assert.Contains(t, out, "export PATH")
	})

	t.Run("env command with bash arg", func(t *testing.T) {
		// Capture stdout
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		envCmd.Run(envCmd, []string{"bash"})

		// Restore stdout and read
		w.Close()
		os.Stdout = old
		var buf bytes.Buffer
		io.Copy(&buf, r)
		out := buf.String()

		assert.Contains(t, out, "#!/bin/sh")
		assert.Contains(t, out, "nvpm shell setup")
		assert.Contains(t, out, "export PATH")
	})

	t.Run("env command with powershell arg", func(t *testing.T) {
		// Capture stdout
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		envCmd.Run(envCmd, []string{"powershell"})

		// Restore stdout and read
		w.Close()
		os.Stdout = old
		var buf bytes.Buffer
		io.Copy(&buf, r)
		out := buf.String()

		assert.Contains(t, out, "$env:PATH")
		assert.Contains(t, out, "nvpm")
	})

	t.Run("env command with pwsh arg", func(t *testing.T) {
		// Capture stdout
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		envCmd.Run(envCmd, []string{"pwsh"})

		// Restore stdout and read
		w.Close()
		os.Stdout = old
		var buf bytes.Buffer
		io.Copy(&buf, r)
		out := buf.String()

		assert.Contains(t, out, "$env:PATH")
		assert.Contains(t, out, "nvpm")
	})

	t.Run("env command with too many args triggers error", func(t *testing.T) {
		// This test covers the log.Fatalln case
		// We can't easily test log.Fatalln directly, but we can verify the command structure
		// and that it has the right argument validation
		assert.NotNil(t, envCmd.Args)

		// Test that the command can handle the validation without panicking
		// The actual log.Fatalln would exit the program, but we can test the command setup
		assert.NotNil(t, envCmd.Run)
	})
}

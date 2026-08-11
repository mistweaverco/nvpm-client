package shell_out

import (
	"io"
	"os"
	"os/exec"
)

func ShellOut(command string, args []string, dir string, env []string) (int, error) {
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if env != nil {
		env = append(env, os.Environ()...)
		cmd.Env = append(cmd.Env, env...)
	}
	err := cmd.Run()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return exitError.ExitCode(), err
		}
		return -1, err
	}
	return 0, nil
}

func HasCommand(command string, args []string, env []string) bool {
	cmd := exec.Command(command, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if env != nil {
		env = append(env, os.Environ()...)
		cmd.Env = append(cmd.Env, env...)
	}
	err := cmd.Run()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return exitError.ExitCode() == 0
		}
		return false
	}
	return true
}

// ShellOutCapture runs a command and captures its exit code and
// output without printing it to stdout or stderr.
func ShellOutCapture(command string, args []string, dir string, env []string) (int, string, error) {
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	if env != nil {
		env = append(env, os.Environ()...)
		cmd.Env = append(cmd.Env, env...)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return exitError.ExitCode(), string(output), err
		}
		return -1, string(output), err
	}
	return 0, string(output), nil
}

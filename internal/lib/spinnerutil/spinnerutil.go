// Package spinnerutil wraps github.com/charmbracelet/huh/spinner for shared CLI patterns.
package spinnerutil

import (
	"fmt"
	"os"
	"sync/atomic"

	"github.com/charmbracelet/huh/spinner"
	"github.com/mattn/go-isatty"
)

var spinnerDepth int32

func isTTY() bool {
	return isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsTerminal(os.Stdout.Fd())
}

// ResetTerminal restores common terminal attributes after Bubble Tea spinners.
// Sequential spinners or glamour rendering can otherwise leave a hidden cursor / raw mode.
func ResetTerminal() {
	if !isTTY() {
		return
	}
	const reset = "\x1b[0m\x1b[?25h\x1b[?1049l"
	_, _ = fmt.Fprint(os.Stderr, reset)
	_, _ = fmt.Fprint(os.Stdout, reset)
}

// Run shows a huh spinner with title while action runs.
// When another Run is already active (nested), a second Bubble Tea program would corrupt the
// terminal; nested calls print the title and run the action without a spinner.
func Run(title string, action func()) error {
	n := atomic.AddInt32(&spinnerDepth, 1)
	defer atomic.AddInt32(&spinnerDepth, -1)
	if n > 1 {
		if isTTY() {
			_, _ = fmt.Fprintf(os.Stderr, "%s\n", title)
		}
		action()
		return nil
	}
	if !isTTY() {
		_, _ = fmt.Fprintf(os.Stdout, "%s\n", title)
		action()
		return nil
	}
	err := spinner.New().Title(title).Action(action).Run()
	ResetTerminal()
	return err
}

// RunIfTTY runs action inside a spinner only when a terminal is available; otherwise prints the
// title and runs the action (useful for CI / logs).
func RunIfTTY(title string, action func()) error {
	if !isTTY() {
		_, _ = fmt.Fprintf(os.Stdout, "%s\n", title)
		action()
		return nil
	}
	return Run(title, action)
}

// RunWithTTYOrPlain runs action with a spinner when a terminal is available; otherwise runs
// plainBefore (if non-nil) then action.
func RunWithTTYOrPlain(title string, plainBefore func(), action func()) error {
	if !isTTY() {
		if plainBefore != nil {
			plainBefore()
		}
		action()
		return nil
	}
	return Run(title, action)
}

// Package spinnerutil wraps github.com/charmbracelet/huh/spinner for shared CLI patterns.
package spinnerutil

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh/spinner"
	"github.com/mattn/go-isatty"
	"github.com/mistweaverco/nvpm-client/internal/lib/log"
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

// IsInterrupted reports whether err is from Ctrl+C / SIGINT (or a canceled parent context).
func IsInterrupted(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, tea.ErrInterrupted) || errors.Is(err, context.Canceled)
}

// Run shows a huh spinner with title while action runs.
// When another Run is already active (nested), a second Bubble Tea program would corrupt the
// terminal; nested calls print the title and run the action without a spinner.
// Ctrl+C / SIGINT stops the spinner UI, waits for the action to finish, then returns an
// error for which IsInterrupted is true.
//
// When NVPM_DEBUG enables verbose logging, the spinner is skipped so log lines on stderr
// remain readable.
func Run(title string, action func()) error {
	return RunContext(context.Background(), title, func(context.Context) error {
		action()
		return nil
	})
}

// RunContext is like Run but passes ctx to the spinner and action. Canceling ctx (or Ctrl+C)
// stops the spinner; the action should check ctx when it can exit early.
func RunContext(ctx context.Context, title string, action func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Skip Bubble Tea spinner when debugging: it owns the terminal and hides slog output.
	if log.VerboseEnabled() || !isTTY() {
		_, _ = fmt.Fprintf(os.Stdout, "%s\n", title)
		return action(ctx)
	}

	n := atomic.AddInt32(&spinnerDepth, 1)
	defer atomic.AddInt32(&spinnerDepth, -1)
	if n > 1 {
		_, _ = fmt.Fprintf(os.Stderr, "%s\n", title)
		return action(ctx)
	}

	var (
		wg      sync.WaitGroup
		actErr  error
		started atomic.Bool
	)
	wg.Add(1)
	err := spinner.New().Title(title).Context(ctx).ActionWithErr(func(c context.Context) error {
		started.Store(true)
		defer wg.Done()
		actErr = action(c)
		return actErr
	}).Run()
	if started.Load() {
		// If Ctrl+C quit the spinner early, the action may still be running - wait for it.
		wg.Wait()
	} else {
		wg.Done()
	}
	ResetTerminal()
	if IsInterrupted(err) {
		return err
	}
	if actErr != nil {
		return actErr
	}
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

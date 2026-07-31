package spinnerutil

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestIsInterrupted(t *testing.T) {
	assert.False(t, IsInterrupted(nil))
	assert.False(t, IsInterrupted(errors.New("other")))
	assert.True(t, IsInterrupted(tea.ErrInterrupted))
	assert.True(t, IsInterrupted(context.Canceled))
	assert.True(t, IsInterrupted(fmtWrap(tea.ErrInterrupted)))
}

func fmtWrap(err error) error {
	return errors.Join(errors.New("wrap"), err)
}

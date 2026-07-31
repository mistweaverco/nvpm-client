package providers

import "sync"

var (
	lastErrorMu sync.Mutex
	lastError   string
)

// ClearLastError clears the last provider operation error.
func ClearLastError() {
	lastErrorMu.Lock()
	defer lastErrorMu.Unlock()
	lastError = ""
}

// SetLastError records a user-facing error from the latest provider operation.
func SetLastError(msg string) {
	lastErrorMu.Lock()
	defer lastErrorMu.Unlock()
	lastError = msg
}

// TakeLastError returns and clears the last provider error, if any.
func TakeLastError() string {
	lastErrorMu.Lock()
	defer lastErrorMu.Unlock()
	msg := lastError
	lastError = ""
	return msg
}

// PeekLastError returns the last provider error without clearing it.
func PeekLastError() string {
	lastErrorMu.Lock()
	defer lastErrorMu.Unlock()
	return lastError
}

func logAndSetError(msg string) {
	SetLastError(msg)
	Logger.Error(msg)
}

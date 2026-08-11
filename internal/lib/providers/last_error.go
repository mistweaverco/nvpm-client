package providers

import "sync"

var (
	lastErrorMu sync.Mutex
	lastError   string
	lastSkip    string
)

// ClearLastError clears the last provider operation error and skip notice.
func ClearLastError() {
	lastErrorMu.Lock()
	defer lastErrorMu.Unlock()
	lastError = ""
	lastSkip = ""
}

// SetLastError records a user-facing error from the latest provider operation.
func SetLastError(msg string) {
	lastErrorMu.Lock()
	defer lastErrorMu.Unlock()
	lastError = msg
	lastSkip = ""
}

// SetLastSkip records a non-failure skip reason (e.g. min-release-age wait).
func SetLastSkip(msg string) {
	lastErrorMu.Lock()
	defer lastErrorMu.Unlock()
	lastSkip = msg
	lastError = ""
}

// TakeLastError returns and clears the last provider error, if any.
func TakeLastError() string {
	lastErrorMu.Lock()
	defer lastErrorMu.Unlock()
	msg := lastError
	lastError = ""
	return msg
}

// TakeLastSkip returns and clears the last skip notice, if any.
func TakeLastSkip() string {
	lastErrorMu.Lock()
	defer lastErrorMu.Unlock()
	msg := lastSkip
	lastSkip = ""
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

package providers

// KindNeovimPlugin is stored in nvpm-lock.json extras.kind for Neovim plugins.
const KindNeovimPlugin = "neovim-plugin"

var currentInstallKind string

// SetInstallKind sets the kind for the next provider Install call (e.g. KindNeovimPlugin).
func SetInstallKind(kind string) {
	currentInstallKind = kind
}

// GetInstallKind returns the install kind for the current operation.
func GetInstallKind() string {
	return currentInstallKind
}

// ResetInstallKind clears the install kind after an operation.
func ResetInstallKind() {
	currentInstallKind = ""
}

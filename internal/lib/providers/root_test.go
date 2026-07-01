package providers

import (
	"os"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/stretchr/testify/assert"
)

func TestDetectProviderUnsupported(t *testing.T) {
	assert.Equal(t, ProviderUnsupported, detectProvider("invalid"))
	assert.Equal(t, ProviderUnsupported, detectProvider("pkg:unknown/pkg"))
	// Missing trailing slash segment triggers len(parts) < 2 path
	assert.Equal(t, ProviderUnsupported, detectProvider("pkg:noslash"))
	// Only prefix
	assert.Equal(t, ProviderUnsupported, detectProvider("pkg:"))
}

func TestSyncAllInvokesProviderSyncs(t *testing.T) {
	_ = withTempNvpmHome(t)
	// Make Go available and Cargo available so their Sync methods run and return fast
	oldGo := goShellOut
	oldCargoHas := cargoHasCommand
	goShellOut = func(string, []string, string, []string) (int, error) { return 0, nil }
	cargoHasCommand = func(string, []string, []string) bool { return true }
	defer func() { goShellOut = oldGo; cargoHasCommand = oldCargoHas }()

	// Ensure base packages dir exists to avoid mkdir races in different providers
	_ = os.MkdirAll(files.GetAppPackagesPath(), 0755)

	// Call SyncAllFromLock; with empty desired sets, each provider's Sync should no-op/return quickly
	_ = SyncAllFromLock()
}

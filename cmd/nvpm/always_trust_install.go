package nvpm

import (
	"fmt"

	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/providers"
)

var (
	installAlwaysTrust   bool
	installNoAlwaysTrust bool
)

func validateAlwaysTrustFlags() error {
	if installAlwaysTrust && installNoAlwaysTrust {
		return fmt.Errorf("--always-trust and --no-always-trust are mutually exclusive")
	}
	return nil
}

func alwaysTrustFlagChanged() bool {
	return installAlwaysTrust || installNoAlwaysTrust
}

func applyPendingAlwaysTrust(sourceID string) {
	if !alwaysTrustFlagChanged() {
		return
	}
	providers.SetPendingAlwaysTrust(sourceID, installAlwaysTrust)
}

func persistAlwaysTrustAfterInstall(sourceID string) {
	if !alwaysTrustFlagChanged() {
		providers.ClearPendingAlwaysTrust(sourceID)
		return
	}
	_ = local_packages_parser.MergePackageAlwaysTrust(sourceID, installAlwaysTrust)
	providers.ClearPendingAlwaysTrust(sourceID)
}

func clearPendingAlwaysTrust(sourceID string) {
	providers.ClearPendingAlwaysTrust(sourceID)
}

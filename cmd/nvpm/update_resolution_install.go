package nvpm

import (
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/providers"
)

var installUpdateResolution string

func applyPendingUpdateResolution(sourceID string) error {
	raw := strings.TrimSpace(installUpdateResolution)
	if raw == "" || !providers.IsGitHostedSourceID(sourceID) {
		return nil
	}
	policy, err := providers.ParseUpdateResolutionFlag(raw)
	if err != nil {
		return err
	}
	providers.SetPendingUpdateResolution(sourceID, policy)
	return nil
}

func persistUpdateResolutionAfterInstall(sourceID string) {
	raw := strings.TrimSpace(installUpdateResolution)
	if raw == "" || !providers.IsGitHostedSourceID(sourceID) {
		providers.ClearPendingUpdateResolution(sourceID)
		return
	}
	policy, err := providers.ParseUpdateResolutionFlag(raw)
	if err == nil {
		_ = local_packages_parser.MergePackageUpdateResolution(
			sourceID,
			local_packages_parser.LockUpdateResolutionFromConfig(policy),
		)
	}
	providers.ClearPendingUpdateResolution(sourceID)
}

func clearPendingUpdateResolution(sourceID string) {
	providers.ClearPendingUpdateResolution(sourceID)
}

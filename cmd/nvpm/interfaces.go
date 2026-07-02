package nvpm

import (
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
)

// LocalPackagesProvider defines the interface for getting local packages data
type LocalPackagesProvider interface {
	GetData(force bool) local_packages_parser.LocalPackageRoot
}

// RegistryProvider defines the interface for getting registry data
type RegistryProvider interface {
	GetData(force bool) []registry_parser.RegistryItem
	GetLatestVersion(sourceID string) string
	// GetLatestVersions returns the latest stable and prerelease versions
	// for the given source ID. Implementations may return empty strings when
	// no data is available.
	GetLatestVersions(sourceID string) (stable string, prerelease string)
}

// UpdateChecker defines the interface for checking if updates are available
type UpdateChecker interface {
	CheckIfUpdateIsAvailable(currentVersion, latestVersion string) (bool, string)
}

// FileDownloader defines the interface for downloading files
type FileDownloader interface {
	// DownloadAndUnzipRegistry returns whether the local registry snapshot was rebuilt.
	DownloadAndUnzipRegistry() (bool, error)
	// DownloadAndUnzipRegistryQuiet is like DownloadAndUnzipRegistry but does not show
	// its own download spinner. Use when the caller already wraps the operation.
	DownloadAndUnzipRegistryQuiet() (bool, error)
}

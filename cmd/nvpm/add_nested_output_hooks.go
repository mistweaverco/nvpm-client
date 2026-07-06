package nvpm

import (
	"fmt"
	"os"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/providers"
)

// registerNestedInstallOutputHooks wires stdout for installs triggered while another
// install is already in progress (e.g. registry-driven dependency installs),
// which bypass the normal per-package spinner messages.
func registerNestedInstallOutputHooks() func() {
	prevStart := providers.NeovimInheritInstallNotifierStart
	prevDone := providers.NeovimInheritInstallNotifierDone
	cleanup := func() {
		providers.NeovimInheritInstallNotifierStart = prevStart
		providers.NeovimInheritInstallNotifierDone = prevDone
	}
	if ShouldUseJSONOutput() {
		return cleanup
	}
	providers.NeovimInheritInstallNotifierStart = func(sourceID, registryVersion string) {
		dispVer := strings.TrimSpace(registryVersion)
		if dispVer == "" {
			dispVer = "latest"
		}
		fmt.Fprintf(os.Stdout, "\nInstalling inherited tree-sitter grammar %s@%s...\n", sourceID, dispVer)
	}
	providers.NeovimInheritInstallNotifierDone = func(sourceID, resolvedVersion string) {
		v := strings.TrimSpace(resolvedVersion)
		if v == "" {
			v = "unknown"
		}
		fmt.Printf("%s Installed inherited grammar %s@%s\n", IconCheck(), sourceID, v)
		for _, line := range providers.ConsumeIntegrationReport(sourceID, v) {
			fmt.Printf("  %s@%s: %s\n", sourceID, v, line)
		}
	}
	return cleanup
}

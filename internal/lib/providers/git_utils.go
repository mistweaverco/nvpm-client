package providers

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
)

// IsGitCommitHash checks if a string is a valid Git commit hash (40 hexadecimal characters)
func IsGitCommitHash(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// DetectRegistryTarget detects the current platform and returns the registry target string
// Registry targets: darwin_arm64, darwin_x64, linux_x64, linux_arm64, linux_arm, win_x64, etc.
func DetectRegistryTarget() string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	var osPart string
	var archPart string

	switch goos {
	case "darwin":
		osPart = "darwin"
	case "linux":
		osPart = "linux"
	case "windows":
		osPart = "win"
	default:
		osPart = strings.ToLower(goos)
	}

	switch goarch {
	case "amd64":
		archPart = "x64"
	case "386":
		archPart = "x86"
	case "arm64":
		archPart = "arm64"
	case "arm":
		archPart = "arm"
	default:
		archPart = strings.ToLower(goarch)
	}

	// Handle special cases
	if goos == "linux" && goarch == "arm" {
		archPart = "arm" // Could be armv6hf, armv7, etc. - registry may specify more precisely
	}

	return fmt.Sprintf("%s_%s", osPart, archPart)
}

// MatchesTarget checks if a registry target matches the current platform
// target can be a string like "linux_x64" or an array like ["darwin_x64", "darwin_arm64"]
func MatchesTarget(target interface{}, currentTarget string) bool {
	switch v := target.(type) {
	case string:
		return v == currentTarget
	case []interface{}:
		for _, t := range v {
			if str, ok := t.(string); ok && str == currentTarget {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// FindMatchingAsset finds the asset entry that matches the current platform
func FindMatchingAsset(assets registry_parser.RegistryItemSourceAssetList) *registry_parser.RegistryItemSourceAsset {
	currentTarget := DetectRegistryTarget()

	for i := range assets {
		if MatchesTarget(assets[i].Target, currentTarget) {
			return &assets[i]
		}
	}

	// Try fallback: check for linux_x64_gnu if linux_x64 not found
	if strings.HasPrefix(currentTarget, "linux_") {
		fallbackTarget := currentTarget + "_gnu"
		for i := range assets {
			if MatchesTarget(assets[i].Target, fallbackTarget) {
				return &assets[i]
			}
		}
	}

	return nil
}

// ResolveTemplate resolves template variables in strings
// Currently supports: {{version}}
func ResolveTemplate(template string, version string) string {
	result := template
	result = strings.ReplaceAll(result, "{{version}}", version)
	result = strings.ReplaceAll(result, "{{ version }}", version)

	// Handle strip_prefix filter: {{ version | strip_prefix "v" }}
	// Simple implementation: if version starts with "v", remove it
	if strings.HasPrefix(version, "v") {
		result = strings.ReplaceAll(result, "{{ version | strip_prefix \"v\" }}", strings.TrimPrefix(version, "v"))
		result = strings.ReplaceAll(result, "{{version | strip_prefix \"v\"}}", strings.TrimPrefix(version, "v"))
	}

	return result
}

// extractBinFromAsset extracts binary name(s) from asset bin field
// bin can be a string (single binary) or a map[string]string (multiple binaries)
func extractBinFromAsset(bin interface{}, binName string) string {
	switch v := bin.(type) {
	case string:
		return v
	case map[string]interface{}:
		if val, ok := v[binName].(string); ok {
			return val
		}
		return ""
	default:
		return ""
	}
}

// ResolveBinPath resolves the binary path from registry bin template
// Examples: "{{source.asset.bin}}" -> "shellcheck"
//
//	"{{source.asset.file}}" -> "latexindent-macos"
//	"{{source.asset.bin.protolint}}" -> "protolint"
func ResolveBinPath(binTemplate string, asset *registry_parser.RegistryItemSourceAsset, binName string) string {
	result := binTemplate

	// Handle {{source.asset.bin}}
	if strings.Contains(result, "{{source.asset.bin}}") {
		binValue := extractBinFromAsset(asset.Bin, binName)
		if binValue == "" {
			// If bin is a string, use it directly
			if str, ok := asset.Bin.(string); ok {
				binValue = str
			}
		}
		result = strings.ReplaceAll(result, "{{source.asset.bin}}", binValue)
	}

	// Handle {{source.asset.bin.<name>}}
	if strings.Contains(result, "{{source.asset.bin.") {
		// Extract bin name from template
		start := strings.Index(result, "{{source.asset.bin.")
		end := strings.Index(result[start:], "}}")
		if end > 0 {
			binKey := result[start+len("{{source.asset.bin.") : start+end]
			binValue := extractBinFromAsset(asset.Bin, binKey)
			result = strings.ReplaceAll(result, "{{source.asset.bin."+binKey+"}}", binValue)
		}
	}

	// Handle {{source.asset.file}}
	if strings.Contains(result, "{{source.asset.file}}") {
		result = strings.ReplaceAll(result, "{{source.asset.file}}", asset.File.String())
	}

	return result
}

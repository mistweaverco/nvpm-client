package providers

import (
	"fmt"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/shell_out"
)

// ProviderPrerequisite describes external tools required by a provider.
type ProviderPrerequisite struct {
	Provider     string
	Description  string
	Website      string
	InstallGuide string
	// CommandSets lists alternative ways to verify the tool is available.
	// The first command set that passes wins (e.g. pip3 or pip).
	CommandSets [][]string
}

var providerPrerequisites = []ProviderPrerequisite{
	{
		Provider:     "npm",
		Description:  "Node.js package manager for JavaScript packages",
		Website:      "https://nodejs.org/",
		InstallGuide: "https://nodejs.org/en/download/package-manager",
		CommandSets:  [][]string{{"npm", "--version"}},
	},
	{
		Provider:     "pypi",
		Description:  "Python package manager for Python packages",
		Website:      "https://www.python.org/",
		InstallGuide: "https://pip.pypa.io/en/stable/installation/",
		CommandSets:  [][]string{{"pip3", "--version"}, {"pip", "--version"}},
	},
	{
		Provider:     "golang",
		Description:  "Go toolchain for Go modules and binaries",
		Website:      "https://go.dev/",
		InstallGuide: "https://go.dev/doc/install",
		CommandSets:  [][]string{{"go", "version"}},
	},
	{
		Provider:     "cargo",
		Description:  "Rust package manager for Cargo crates",
		Website:      "https://www.rust-lang.org/",
		InstallGuide: "https://rustup.rs/",
		CommandSets:  [][]string{{"cargo", "--version"}},
	},
	{
		Provider:     "github",
		Description:  "Git for cloning GitHub repositories",
		Website:      "https://github.com/",
		InstallGuide: "https://git-scm.com/downloads",
		CommandSets:  [][]string{{"git", "--version"}},
	},
	{
		Provider:     "gitlab",
		Description:  "Git for cloning GitLab repositories",
		Website:      "https://about.gitlab.com/",
		InstallGuide: "https://git-scm.com/downloads",
		CommandSets:  [][]string{{"git", "--version"}},
	},
	{
		Provider:     "codeberg",
		Description:  "Git for cloning Codeberg repositories",
		Website:      "https://codeberg.org/",
		InstallGuide: "https://git-scm.com/downloads",
		CommandSets:  [][]string{{"git", "--version"}},
	},
	{
		Provider:     "gem",
		Description:  "RubyGems for Ruby packages",
		Website:      "https://rubygems.org/",
		InstallGuide: "https://www.ruby-lang.org/en/documentation/installation/",
		CommandSets:  [][]string{{"gem", "--version"}},
	},
	{
		Provider:     "composer",
		Description:  "Composer for PHP packages",
		Website:      "https://getcomposer.org/",
		InstallGuide: "https://getcomposer.org/download/",
		CommandSets:  [][]string{{"composer", "--version"}},
	},
	{
		Provider:     "luarocks",
		Description:  "LuaRocks for Lua packages",
		Website:      "https://luarocks.org/",
		InstallGuide: "https://github.com/luarocks/luarocks/wiki/Download",
		CommandSets:  [][]string{{"luarocks", "--version"}},
	},
	{
		Provider:     "nuget",
		Description:  ".NET SDK for NuGet global tools",
		Website:      "https://dotnet.microsoft.com/",
		InstallGuide: "https://dotnet.microsoft.com/download",
		CommandSets:  [][]string{{"dotnet", "--version"}},
	},
	{
		Provider:     "opam",
		Description:  "OPAM for OCaml packages",
		Website:      "https://opam.ocaml.org/",
		InstallGuide: "https://opam.ocaml.org/doc/Install.html",
		CommandSets:  [][]string{{"opam", "--version"}},
	},
	{
		Provider:     "openvsx",
		Description:  "VS Code CLI for Open VSX extensions",
		Website:      "https://open-vsx.org/",
		InstallGuide: "https://code.visualstudio.com/docs/editor/command-line",
		CommandSets:  [][]string{{"code", "--version"}},
	},
	{
		Provider:    "generic",
		Description: "Generic provider (no external tools required)",
	},
}

func prerequisiteByProvider(name string) (ProviderPrerequisite, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, spec := range providerPrerequisites {
		if spec.Provider == name {
			return spec, true
		}
	}
	return ProviderPrerequisite{}, false
}

func prerequisiteAvailable(spec ProviderPrerequisite) (bool, string) {
	if len(spec.CommandSets) == 0 {
		return true, ""
	}
	for _, cmdSet := range spec.CommandSets {
		if len(cmdSet) == 0 {
			continue
		}
		cmd := cmdSet[0]
		args := cmdSet[1:]
		if shell_out.HasCommand(cmd, args, nil) {
			return true, ""
		}
	}
	missing := spec.CommandSets[0][0]
	for _, cmdSet := range spec.CommandSets[1:] {
		if len(cmdSet) > 0 {
			missing += " or " + cmdSet[0]
		}
	}
	return false, missing
}

// CheckProviderPrerequisites verifies that external tools for a provider are available.
func CheckProviderPrerequisites(providerName string) error {
	spec, ok := prerequisiteByProvider(providerName)
	if !ok {
		return fmt.Errorf("unknown provider %q", providerName)
	}
	available, missing := prerequisiteAvailable(spec)
	if available {
		return nil
	}
	return prerequisiteError(spec, missing)
}

// CheckSourceIDPrerequisites verifies prerequisites for a package source ID.
func CheckSourceIDPrerequisites(sourceID string) error {
	providerName, _ := extractProviderAndPackage(normalizePackageID(sourceID))
	if providerName == "" {
		return fmt.Errorf("invalid package id %q: expected <provider>:<package-id>", sourceID)
	}
	return CheckProviderPrerequisites(providerName)
}

func prerequisiteError(spec ProviderPrerequisite, missingTool string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s provider requires %s, but %q was not found in PATH.\n\n", strings.ToUpper(spec.Provider), spec.Description, missingTool)
	b.WriteString("To fix this:\n")
	if spec.InstallGuide != "" {
		fmt.Fprintf(&b, "  • Install guide: %s\n", spec.InstallGuide)
	}
	if spec.Website != "" && spec.Website != spec.InstallGuide {
		fmt.Fprintf(&b, "  • Website: %s\n", spec.Website)
	}
	b.WriteString("\nRun 'nvpm health' for a full provider requirements check.")
	return fmt.Errorf("%s", strings.TrimRight(b.String(), "\n"))
}

// ProviderPrerequisiteHelp returns human-readable setup help for a provider.
func ProviderPrerequisiteHelp(providerName string) string {
	spec, ok := prerequisiteByProvider(providerName)
	if !ok {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", spec.Description)
	if spec.Website != "" {
		fmt.Fprintf(&b, "Website: %s\n", spec.Website)
	}
	if spec.InstallGuide != "" {
		fmt.Fprintf(&b, "Install: %s\n", spec.InstallGuide)
	}
	if len(spec.CommandSets) > 0 {
		fmt.Fprintf(&b, "Required command: %s\n", formatCommandSets(spec.CommandSets))
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatCommandSets(sets [][]string) string {
	parts := make([]string, 0, len(sets))
	for _, cmdSet := range sets {
		if len(cmdSet) == 0 {
			continue
		}
		parts = append(parts, strings.Join(cmdSet, " "))
	}
	return strings.Join(parts, " OR ")
}

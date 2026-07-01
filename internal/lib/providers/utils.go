package providers

import "github.com/mistweaverco/nvpm-client/internal/lib/shell_out"

// ProviderHealthStatus represents the health status of a single provider
type ProviderHealthStatus struct {
	Provider     string `json:"provider"`
	Available    bool   `json:"available"`
	RequiredTool string `json:"required_tool,omitempty"`
	Description  string `json:"description"`
}

// CheckAllProvidersHealth checks all providers and returns their health status
func CheckAllProvidersHealth() []ProviderHealthStatus {
	var statuses []ProviderHealthStatus

	// Check each provider
	providers := []struct {
		name        string
		requiredCmd []string // Command and args to check
		description string
	}{
		{"npm", []string{"npm", "--version"}, "Node.js package manager for JavaScript packages"},
		{"pypi", []string{"pip3", "--version"}, "Python package manager for Python packages"},
		{"golang", []string{"go", "version"}, "Go programming language for Go packages"},
		{"cargo", []string{"cargo", "--version"}, "Rust package manager for Rust packages"},
		{"github", []string{"git", "--version"}, "Git for GitHub repository packages"},
		{"gitlab", []string{"git", "--version"}, "Git for GitLab repository packages"},
		{"codeberg", []string{"git", "--version"}, "Git for Codeberg repository packages"},
		{"gem", []string{"gem", "--version"}, "RubyGems for Ruby packages"},
		{"composer", []string{"composer", "--version"}, "Composer for PHP packages"},
		{"luarocks", []string{"luarocks", "--version"}, "LuaRocks for Lua packages"},
		{"nuget", []string{"dotnet", "--version"}, ".NET SDK for NuGet packages"},
		{"opam", []string{"opam", "--version"}, "OPAM for OCaml packages"},
		{"openvsx", []string{"code", "--version"}, "VS Code CLI for OpenVSX extensions"},
		{"generic", nil, "Generic provider (no specific tools required)"},
	}

	for _, p := range providers {
		available := true
		var requiredTool string
		if len(p.requiredCmd) > 0 {
			cmd := p.requiredCmd[0]
			args := p.requiredCmd[1:]
			available = shell_out.HasCommand(cmd, args, nil)
			requiredTool = cmd
			// Special handling for PyPI - check both pip3 and pip
			if p.name == "pypi" && !available {
				available = shell_out.HasCommand("pip", []string{"--version"}, nil)
				if available {
					requiredTool = "pip"
				}
			}
		}

		status := ProviderHealthStatus{
			Provider:    p.name,
			Available:   available,
			Description: p.description,
		}

		if !available && requiredTool != "" {
			status.RequiredTool = requiredTool
		}

		statuses = append(statuses, status)
	}

	return statuses
}

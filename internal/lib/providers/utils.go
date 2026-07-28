package providers

// ProviderHealthStatus represents the health status of a single provider
type ProviderHealthStatus struct {
	Provider     string `json:"provider"`
	Available    bool   `json:"available"`
	RequiredTool string `json:"required_tool,omitempty"`
	Description  string `json:"description"`
	Website      string `json:"website,omitempty"`
	InstallGuide string `json:"install_guide,omitempty"`
	Help         string `json:"help,omitempty"`
}

// CheckAllProvidersHealth checks all providers and returns their health status
func CheckAllProvidersHealth() []ProviderHealthStatus {
	statuses := make([]ProviderHealthStatus, 0, len(providerPrerequisites))
	for _, spec := range providerPrerequisites {
		available, missing := prerequisiteAvailable(spec)
		status := ProviderHealthStatus{
			Provider:     spec.Provider,
			Available:    available,
			Description:  spec.Description,
			Website:      spec.Website,
			InstallGuide: spec.InstallGuide,
		}
		if !available && missing != "" {
			status.RequiredTool = missing
			status.Help = ProviderPrerequisiteHelp(spec.Provider)
		}
		statuses = append(statuses, status)
	}
	return statuses
}

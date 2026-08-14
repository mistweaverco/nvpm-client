package providers

import "strings"

// IntegrationReportLine is a user-facing note from an editor integration.
type IntegrationReportLine struct {
	Text    string
	Warning bool
}

// integrationReports is a best-effort side-channel for the CLI to display
// where integrations installed things.
// Key format: "<sourceID>@<version>"
var integrationReports = map[string][]IntegrationReportLine{}

func integrationReportKey(sourceID, version string) string {
	return strings.TrimSpace(sourceID) + "@" + strings.TrimSpace(version)
}

func addIntegrationReport(sourceID, version, line string, warning bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	k := integrationReportKey(sourceID, version)
	integrationReports[k] = append(integrationReports[k], IntegrationReportLine{
		Text:    line,
		Warning: warning,
	})
}

func AddIntegrationReportLine(sourceID, version, line string) {
	addIntegrationReport(sourceID, version, line, false)
}

func AddIntegrationReportWarning(sourceID, version, line string) {
	addIntegrationReport(sourceID, version, line, true)
}

// ConsumeIntegrationReport exposes per-install integration messages for the CLI.
// It is best-effort; when no integrations ran, it returns nil/empty.
func ConsumeIntegrationReport(sourceID, version string) []IntegrationReportLine {
	k := integrationReportKey(sourceID, version)
	lines := integrationReports[k]
	delete(integrationReports, k)
	return lines
}

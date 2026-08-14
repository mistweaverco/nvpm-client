package providers

import "testing"

func TestIntegrationReport_Consume(t *testing.T) {
	AddIntegrationReportLine("github:x/y", "v1", "Integrated into Neovim: /tmp")
	lines := ConsumeIntegrationReport("github:x/y", "v1")
	if len(lines) != 1 || lines[0].Text != "Integrated into Neovim: /tmp" || lines[0].Warning {
		t.Fatalf("expected success line, got %#v", lines)
	}
	AddIntegrationReportWarning("github:x/y", "v1", "query warning")
	warns := ConsumeIntegrationReport("github:x/y", "v1")
	if len(warns) != 1 || !warns[0].Warning || warns[0].Text != "query warning" {
		t.Fatalf("expected warning line, got %#v", warns)
	}
	again := ConsumeIntegrationReport("github:x/y", "v1")
	if len(again) != 0 {
		t.Fatalf("expected empty after consume, got %#v", again)
	}
}

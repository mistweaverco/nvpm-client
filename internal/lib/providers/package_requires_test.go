package providers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
)

func TestParseRequirePackageRef(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ref      string
		wantID   string
		wantVer  string
		wantFail bool
	}{
		{"npm:tree-sitter-cli", "npm:tree-sitter-cli", "", false},
		{"npm:tree-sitter-cli@0.25.0", "npm:tree-sitter-cli", "0.25.0", false},
		{"github:tree-sitter/tree-sitter@v0.25.0", "github:tree-sitter/tree-sitter", "v0.25.0", false},
		{"npm:@scope/pkg@1.2.3", "npm:@scope/pkg", "1.2.3", false},
		{"pkg:npm/foo@1.0.0", "npm:foo", "1.0.0", false},
		{"", "", "", true},
		{"invalid", "", "", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.ref, func(t *testing.T) {
			t.Parallel()
			id, ver, err := parseRequirePackageRef(tc.ref)
			if tc.wantFail {
				if err == nil {
					t.Fatalf("expected error for %q", tc.ref)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id != tc.wantID || ver != tc.wantVer {
				t.Fatalf("got %q@%q want %q@%q", id, ver, tc.wantID, tc.wantVer)
			}
		})
	}
}

func testRegistryParser(t *testing.T, items registry_parser.RegistryRoot) *registry_parser.RegistryParser {
	t.Helper()
	reg := registry_parser.NewRegistryParser(nil)
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.LoadFromBytes(raw); err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestRequiresInstallOrder_TransitiveAndAll(t *testing.T) {
	reg := testRegistryParser(t, registry_parser.RegistryRoot{
		{
			Name: "app",
			Source: registry_parser.RegistryItemSource{
				ID: "npm:app",
			},
			Requires: &registry_parser.RegistryItemRequires{
				All: []string{"npm:lib-a"},
			},
		},
		{
			Name: "lib-a",
			Source: registry_parser.RegistryItemSource{
				ID: "npm:lib-a",
			},
			Requires: &registry_parser.RegistryItemRequires{
				All: []string{"npm:lib-b"},
			},
		},
		{
			Name: "lib-b",
			Source: registry_parser.RegistryItemSource{
				ID: "npm:lib-b",
			},
		},
	})
	order, err := requiresInstallOrder("npm:app", reg, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"npm:lib-b", "npm:lib-a"}
	if len(order) != len(want) {
		t.Fatalf("order %v want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order %v want %v", order, want)
		}
	}
}

func TestRequiresInstallOrder_Cycle(t *testing.T) {
	reg := testRegistryParser(t, registry_parser.RegistryRoot{
		{
			Name: "a",
			Source: registry_parser.RegistryItemSource{
				ID: "npm:a",
			},
			Requires: &registry_parser.RegistryItemRequires{
				All: []string{"npm:b"},
			},
		},
		{
			Name: "b",
			Source: registry_parser.RegistryItemSource{
				ID: "npm:b",
			},
			Requires: &registry_parser.RegistryItemRequires{
				All: []string{"npm:a"},
			},
		},
	})
	_, err := requiresInstallOrder("npm:a", reg, false)
	if err == nil || !strings.Contains(err.Error(), "cyclic") {
		t.Fatalf("expected cyclic error, got %v", err)
	}
}

func TestExpandRegistryRequires_OneSatisfiedSkips(t *testing.T) {
	prev := packageRequiresIsInstalled
	defer func() { packageRequiresIsInstalled = prev }()
	packageRequiresIsInstalled = func(sourceID string) bool {
		return sourceID == "npm:tree-sitter-cli"
	}
	req := &registry_parser.RegistryItemRequires{
		One: []string{"npm:tree-sitter-cli", "github:tree-sitter/tree-sitter"},
	}
	deps, err := expandRegistryRequires(req, registry_parser.NewRegistryParser(nil), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 0 {
		t.Fatalf("expected no deps when one is satisfied, got %v", deps)
	}
}

func TestExpandRegistryRequires_OneAutoInstallPicksFirst(t *testing.T) {
	prevInstalled := packageRequiresIsInstalled
	defer func() { packageRequiresIsInstalled = prevInstalled }()
	packageRequiresIsInstalled = func(string) bool { return false }
	req := &registry_parser.RegistryItemRequires{
		One: []string{"npm:tree-sitter-cli", "github:tree-sitter/tree-sitter"},
	}
	deps, err := expandRegistryRequires(req, registry_parser.NewRegistryParser(nil), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 || deps[0] != "github:tree-sitter/tree-sitter" {
		t.Fatalf("got %v", deps)
	}
}

func TestExpandRegistryRequires_OnePromptsWhenMissing(t *testing.T) {
	prevInstalled := packageRequiresIsInstalled
	prevPicker := packageRequiresOnePicker
	defer func() {
		packageRequiresIsInstalled = prevInstalled
		packageRequiresOnePicker = prevPicker
	}()
	packageRequiresIsInstalled = func(string) bool { return false }
	packageRequiresOnePicker = func(_ string, options []string) (string, error) {
		return options[0], nil
	}
	req := &registry_parser.RegistryItemRequires{
		One: []string{"npm:tree-sitter-cli", "github:tree-sitter/tree-sitter"},
	}
	deps, err := expandRegistryRequires(req, registry_parser.NewRegistryParser(nil), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 || deps[0] != "github:tree-sitter/tree-sitter" {
		t.Fatalf("got %v", deps)
	}
}

func TestPreflightPackageRequires_PromptsForRequiresOne(t *testing.T) {
	prevInstalled := packageRequiresIsInstalled
	prevInstall := packageRequiresInstallFn
	prevResolve := packageRequiresResolveVersion
	prevPicker := packageRequiresOnePicker
	prevPrompt := packageRequiresPrompt
	defer func() {
		packageRequiresIsInstalled = prevInstalled
		packageRequiresInstallFn = prevInstall
		packageRequiresResolveVersion = prevResolve
		packageRequiresOnePicker = prevPicker
		packageRequiresPrompt = prevPrompt
	}()
	packageRequiresIsInstalled = func(string) bool { return false }
	packageRequiresResolveVersion = func(string, string) (string, error) {
		return "0.25.0", nil
	}
	var installed []string
	packageRequiresInstallFn = func(sourceID, version string) bool {
		installed = append(installed, sourceID)
		return true
	}
	packageRequiresOnePicker = func(_ string, options []string) (string, error) {
		return options[0], nil
	}
	packageRequiresPrompt = func(string, string) (packageRequiresPromptAction, error) {
		return packageRequiresInstall, nil
	}
	packageRequiresIsInstalled = func(id string) bool {
		for _, i := range installed {
			if i == id {
				return true
			}
		}
		return false
	}

	item := registry_parser.RegistryItem{
		Name:       "tree-sitter-svelte",
		Categories: []string{"Tree-sitter-parser"},
		Source:     registry_parser.RegistryItemSource{ID: "github:tree-sitter-grammars/tree-sitter-svelte"},
		Requires: &registry_parser.RegistryItemRequires{
			One: []string{"npm:tree-sitter-cli", "github:tree-sitter/tree-sitter"},
		},
	}
	if err := PreflightPackageRequires(item); err != nil {
		t.Fatal(err)
	}
	if len(installed) != 1 || installed[0] != "github:tree-sitter/tree-sitter" {
		t.Fatalf("installed %v", installed)
	}
}

func TestEnsureLockfilePackageRequires_AutoInstall(t *testing.T) {
	prevInstalled := packageRequiresIsInstalled
	prevInstall := packageRequiresInstallFn
	prevResolve := packageRequiresResolveVersion
	defer func() {
		packageRequiresIsInstalled = prevInstalled
		packageRequiresInstallFn = prevInstall
		packageRequiresResolveVersion = prevResolve
	}()
	packageRequiresResolveVersion = func(string, string) (string, error) {
		return "1.0.0", nil
	}
	installed := map[string]struct{}{"npm:app": {}}
	packageRequiresIsInstalled = func(sourceID string) bool {
		_, ok := installed[sourceID]
		return ok
	}
	var installedOrder []string
	packageRequiresInstallFn = func(sourceID, version string) bool {
		installed[sourceID] = struct{}{}
		installedOrder = append(installedOrder, sourceID)
		return true
	}
	reg := testRegistryParser(t, registry_parser.RegistryRoot{
		{
			Name:   "app",
			Source: registry_parser.RegistryItemSource{ID: "npm:app"},
			Requires: &registry_parser.RegistryItemRequires{
				All: []string{"npm:lib-a"},
			},
		},
		{
			Name:   "lib-a",
			Source: registry_parser.RegistryItemSource{ID: "npm:lib-a"},
		},
	})
	prevReg := packageRequiresNewRegistry
	packageRequiresNewRegistry = func() *registry_parser.RegistryParser { return reg }
	defer func() { packageRequiresNewRegistry = prevReg }()

	lock := local_packages_parser.LocalPackageRoot{
		Packages: []local_packages_parser.LocalPackageItem{
			{SourceID: "npm:app", Version: "1.0.0"},
		},
	}
	prevLock := packageRequiresLockData
	packageRequiresLockData = func(bool) local_packages_parser.LocalPackageRoot { return lock }
	defer func() { packageRequiresLockData = prevLock }()

	if err := EnsureLockfilePackageRequires(true); err != nil {
		t.Fatal(err)
	}
	if len(installedOrder) != 1 || installedOrder[0] != "npm:lib-a" {
		t.Fatalf("installed %v", installedOrder)
	}
}

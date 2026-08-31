package providers

// treeSitterDependencyInstallSuccessCount counts successful Install() calls made from
// tree-sitter inherit resolution (nested grammar installs), for CLI summaries.
var treeSitterDependencyInstallSuccessCount int

// nestedTreeSitterDependencyInstallDepth is > 0 while Install() runs for a parser
// required by another grammar (inherits / injections). Nested GitHub installs skip the
// phased preflight, so this flag auto-allows registry external_queries without a second
// prompt (the parent already opted into Neovim integration).
var nestedTreeSitterDependencyInstallDepth int

// ResetTreeSitterDependencyInstallSuccessCount clears the counter (call once per install command).
func ResetTreeSitterDependencyInstallSuccessCount() {
	treeSitterDependencyInstallSuccessCount = 0
}

func noteTreeSitterDependencyInstallSuccess() {
	treeSitterDependencyInstallSuccessCount++
}

// ConsumeTreeSitterDependencyInstallSuccessCount returns successful dependency installs
// since the last reset, then clears the counter.
func ConsumeTreeSitterDependencyInstallSuccessCount() int {
	n := treeSitterDependencyInstallSuccessCount
	treeSitterDependencyInstallSuccessCount = 0
	return n
}

func inNestedTreeSitterDependencyInstall() bool {
	return nestedTreeSitterDependencyInstallDepth > 0
}

// nestedTreeSitterExternalQueryPreflight allows registry external query clones without
// prompting when this grammar is being installed as a Neovim dependency of another package.
func nestedTreeSitterExternalQueryPreflight() *ExternalQueryPreflightChoice {
	if !inNestedTreeSitterDependencyInstall() || !integrationEnabled("neovim") {
		return nil
	}
	if externalTreeSitterQueriesPolicyValue == externalTreeSitterQueriesNever {
		return &ExternalQueryPreflightChoice{AllowUnpinned: false}
	}
	return &ExternalQueryPreflightChoice{AllowUnpinned: true}
}

// withNestedTreeSitterDependencyInstall runs fn as a nested parser/query dependency install.
// It preserves GitHub phased-install deferred state so a parent html (etc.) preflight is
// not overwritten if the dependency also touches that singleton.
func withNestedTreeSitterDependencyInstall(fn func()) {
	nestedTreeSitterDependencyInstallDepth++
	savedDeferred := githubTreeSitterDeferred
	defer func() {
		githubTreeSitterDeferred = savedDeferred
		nestedTreeSitterDependencyInstallDepth--
	}()
	fn()
}

func installTreeSitterDependencyPackage(sourceID, version string) bool {
	var ok bool
	withNestedTreeSitterDependencyInstall(func() {
		ok = Install(sourceID, version)
	})
	return ok
}

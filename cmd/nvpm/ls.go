package nvpm

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/term"
	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/providers"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/semver"
	"github.com/mistweaverco/nvpm-client/internal/lib/spinnerutil"
	"github.com/spf13/cobra"
)

// showDiscoveryProgress controls whether list commands show discovery progress (spinner/messages).
// Tests disable this to keep golden outputs stable.
var showDiscoveryProgress = true

// showRegistryProgress controls whether list commands show registry refresh progress.
// Tests disable this to keep golden outputs stable.
var showRegistryProgress = true

// ListService handles listing operations with dependency injection
type ListService struct {
	localPackages  LocalPackagesProvider
	registry       RegistryProvider
	updateChecker  UpdateChecker
	fileDownloader FileDownloader
}

// newListServiceFunc is a variable to allow test injection
var newListServiceFunc = func() *ListService {
	return &ListService{
		localPackages:  &defaultLocalPackagesProvider{},
		registry:       &defaultRegistryProvider{},
		updateChecker:  &defaultUpdateChecker{},
		fileDownloader: &defaultFileDownloader{},
	}
}

// NewListService creates a new ListService with default dependencies
func NewListService() *ListService {
	return newListServiceFunc()
}

// NewListServiceWithDependencies creates a new ListService with custom dependencies
func NewListServiceWithDependencies(
	localPackages LocalPackagesProvider,
	registry RegistryProvider,
	updateChecker UpdateChecker,
	fileDownloader FileDownloader,
) *ListService {
	return &ListService{
		localPackages:  localPackages,
		registry:       registry,
		updateChecker:  updateChecker,
		fileDownloader: fileDownloader,
	}
}

var lsCmd = &cobra.Command{
	Use:     "ls [filter...]",
	Aliases: []string{"list"},
	Short:   "List packages",
	Long: `List packages based on the specified options.

By default, shows locally installed packages.
Use --all to show all available packages from the registry.
You can provide filter arguments to show only packages whose names match the filter strings (case-insensitive substring match).

Optional filters (combinable): --only-outdated, --only-providers, --only-categories.`,
	Args: cobra.ArbitraryArgs,
	// Enable shell completion for package names
	ValidArgsFunction: packageIDCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		allFlag, _ := cmd.Flags().GetBool("all")
		opts, err := listQueryOptionsFromFlags(cmd, args)
		if err != nil {
			fmt.Printf("%s %v\n", IconClose(), err)
			os.Exit(1)
		}
		service := newListService()

		if allFlag {
			service.ListAllPackages(opts)
		} else {
			service.ListInstalledPackages(opts)
		}
	},
}

func init() {
	lsCmd.Flags().BoolP("all", "A", false, "List all available packages from the registry")
	lsCmd.Flags().Bool("only-outdated", false, "Show only packages with an update available (with --all: registry entries you have installed that are outdated)")
	lsCmd.Flags().String("only-providers", "", "Comma-separated provider names to include, e.g. pypi,npm")
	lsCmd.Flags().String("only-categories", "", "Comma-separated category tokens; a package matches if any of its registry categories matches any token (substring match, case-insensitive), e.g. lsp,tree-sitter-parser")
	lsCmd.Flags().Bool("only-plugins", false, "Show only Neovim plugins (lock entries with extras.kind neovim-plugin)")
	lsCmd.Flags().Bool("only-always-trusted", false, "Show only packages with extras.always_trust in the lock file")
	registerShowFilterFlag(lsCmd)
}

// ListQueryOptions holds positional name filters plus optional list constraints.
type ListQueryOptions struct {
	NameFilters       []string
	OnlyOutdated      bool
	OnlyProviders     []string // lowercase provider names (validated)
	OnlyCategories    []string // trimmed tokens from --only-categories
	OnlyPlugins       bool     // lock entries with extras.kind neovim-plugin
	OnlyAlwaysTrusted bool     // lock extras.always_trust
	ShowFilters       []string // --filter path:value (AND, match show JSON)
}

func listQueryOptionsFromFlags(cmd *cobra.Command, args []string) (ListQueryOptions, error) {
	opts := ListQueryOptions{NameFilters: args}
	var err error
	opts.OnlyOutdated, _ = cmd.Flags().GetBool("only-outdated")
	onlyProv, _ := cmd.Flags().GetString("only-providers")
	opts.OnlyProviders, err = parseAndValidateOnlyProviders(onlyProv)
	if err != nil {
		return ListQueryOptions{}, err
	}
	onlyCat, _ := cmd.Flags().GetString("only-categories")
	opts.OnlyCategories = parseCommaSeparatedList(onlyCat)
	opts.OnlyPlugins, _ = cmd.Flags().GetBool("only-plugins")
	opts.OnlyAlwaysTrusted, _ = cmd.Flags().GetBool("only-always-trusted")
	opts.ShowFilters, _ = cmd.Flags().GetStringArray("filter")
	return opts, nil
}

func parseCommaSeparatedList(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseAndValidateOnlyProviders(s string) ([]string, error) {
	parts := parseCommaSeparatedList(s)
	if len(parts) == 0 {
		return nil, nil
	}
	valid := make(map[string]struct{}, len(providers.AvailableProviders))
	for _, p := range providers.AvailableProviders {
		valid[strings.ToLower(p)] = struct{}{}
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		pl := strings.ToLower(strings.TrimSpace(p))
		if _, ok := valid[pl]; !ok {
			return nil, fmt.Errorf("unknown provider %q in --only-providers (supported: %s)", p, strings.Join(providers.AvailableProviders, ", "))
		}
		out = append(out, pl)
	}
	return out, nil
}

// registryItemMatchesCategoryFilters is true when any filter token matches any package category
// (case-insensitive equality, or substring match when both sides are at least 3 runes).
func registryItemMatchesCategoryFilters(categories []string, filters []string) bool {
	for _, f := range filters {
		fl := strings.TrimSpace(f)
		if fl == "" {
			continue
		}
		flLower := strings.ToLower(fl)
		for _, c := range categories {
			cl := strings.TrimSpace(c)
			if cl == "" {
				continue
			}
			clLower := strings.ToLower(cl)
			if clLower == flLower {
				return true
			}
			if len(flLower) >= 3 && strings.Contains(clLower, flLower) {
				return true
			}
			if len(clLower) >= 3 && strings.Contains(flLower, clLower) {
				return true
			}
		}
	}
	return false
}

func (o ListQueryOptions) hasAdvancedFilters() bool {
	return o.OnlyOutdated || len(o.OnlyProviders) > 0 || len(o.OnlyCategories) > 0 || o.OnlyPlugins || o.OnlyAlwaysTrusted || len(o.ShowFilters) > 0
}

func (o ListQueryOptions) constraintDescriptionPlain() string {
	if !o.hasAdvancedFilters() {
		return ""
	}
	var parts []string
	if o.OnlyOutdated {
		parts = append(parts, "outdated only")
	}
	if len(o.OnlyProviders) > 0 {
		parts = append(parts, fmt.Sprintf("providers: %s", strings.Join(o.OnlyProviders, ", ")))
	}
	if len(o.OnlyCategories) > 0 {
		parts = append(parts, fmt.Sprintf("categories: %s", strings.Join(o.OnlyCategories, ", ")))
	}
	if o.OnlyPlugins {
		parts = append(parts, "neovim plugins only")
	}
	if o.OnlyAlwaysTrusted {
		parts = append(parts, "always-trusted only")
	}
	if len(o.ShowFilters) > 0 {
		parts = append(parts, fmt.Sprintf("filters: %s", strings.Join(o.ShowFilters, ", ")))
	}
	return " - " + strings.Join(parts, "; ")
}

func (o ListQueryOptions) constraintDescriptionMarkdown() string {
	if !o.hasAdvancedFilters() {
		return ""
	}
	var parts []string
	if o.OnlyOutdated {
		parts = append(parts, "outdated only")
	}
	if len(o.OnlyProviders) > 0 {
		parts = append(parts, fmt.Sprintf("providers: **%s**", strings.Join(o.OnlyProviders, ", ")))
	}
	if len(o.OnlyCategories) > 0 {
		parts = append(parts, fmt.Sprintf("categories: **%s**", strings.Join(o.OnlyCategories, ", ")))
	}
	if o.OnlyPlugins {
		parts = append(parts, "neovim plugins only")
	}
	if o.OnlyAlwaysTrusted {
		parts = append(parts, "always-trusted only")
	}
	if len(o.ShowFilters) > 0 {
		// Backticks: filter values often contain '*' which would break **bold** markdown.
		quoted := make([]string, 0, len(o.ShowFilters))
		for _, f := range o.ShowFilters {
			quoted = append(quoted, "`"+f+"`")
		}
		parts = append(parts, fmt.Sprintf("filters: %s", strings.Join(quoted, ", ")))
	}
	return " - " + strings.Join(parts, "; ")
}

func appendListQueryJSONFields(m map[string]any, o ListQueryOptions) {
	if o.OnlyOutdated {
		m["only_outdated"] = true
	}
	if len(o.OnlyProviders) > 0 {
		m["only_providers"] = append([]string(nil), o.OnlyProviders...)
	}
	if len(o.OnlyCategories) > 0 {
		m["only_categories"] = append([]string(nil), o.OnlyCategories...)
	}
	if o.OnlyPlugins {
		m["only_plugins"] = true
	}
	if o.OnlyAlwaysTrusted {
		m["only_always_trusted"] = true
	}
	if len(o.ShowFilters) > 0 {
		m["filters_dsl"] = append([]string(nil), o.ShowFilters...)
	}
}

// newListService is a factory to allow test injection
var newListService = NewListService

// refreshRegistry ensures the registry is up to date before listing.
// Errors are ignored intentionally so listing still works offline.
func (ls *ListService) refreshRegistry() {
	_, _ = ls.fileDownloader.DownloadAndUnzipRegistry()
}

func (ls *ListService) shouldShowListPrepSpinner() bool {
	return (showRegistryProgress || showDiscoveryProgress) && !ShouldUseJSONOutput()
}

func (ls *ListService) discoveryPairsForInstalled(localPackages []local_packages_parser.LocalPackageItem) []providers.DiscoveryPair {
	if cfg.Flags.MinReleaseAge <= 0 || len(localPackages) == 0 {
		return nil
	}
	pairs := make([]providers.DiscoveryPair, 0, len(localPackages)*2)
	for _, pkg := range localPackages {
		stable, prerelease := ls.registry.GetLatestVersions(pkg.SourceID)
		if stable == "" && prerelease == "" {
			continue
		}

		candidates := make([]string, 0, 2)
		if stable != "" && stable != "latest" && shouldRecordDiscoveredVersion(pkg.Version, stable) {
			candidates = append(candidates, stable)
		}
		if prerelease != "" && prerelease != "latest" && prerelease != stable && shouldRecordDiscoveredVersion(pkg.Version, prerelease) {
			candidates = append(candidates, prerelease)
		}

		for _, v := range candidates {
			pair := providers.DiscoveryPair{SourceID: pkg.SourceID, Version: v}
			if providers.IsGitHostedSourceID(pkg.SourceID) {
				commit, err := providers.ResolveGitDiscoveryCommit(pkg.SourceID, v)
				if err != nil {
					continue
				}
				pair.Commit = commit
			}
			pairs = append(pairs, pair)
		}
	}
	return pairs
}

func shouldRecordDiscoveredVersion(installedVersion, candidate string) bool {
	installedVersion = strings.TrimSpace(installedVersion)
	candidate = strings.TrimSpace(candidate)
	if candidate == "" || candidate == "latest" {
		return false
	}
	if installedVersion == "" || installedVersion == "latest" {
		return true
	}
	return semver.IsGreater(installedVersion, candidate)
}

type discoveryDisplay struct {
	Available    []string
	Discovered   []string
	Eligible     []string
	EligibleSoon []string
}

type gitDiscoveredRef struct {
	Ref       string
	Commit    string
	FirstSeen time.Time
}

func parseGitDiscoveryKeyVersion(version string) (ref string, commit string) {
	version = strings.TrimSpace(version)
	if version == "" {
		return "", ""
	}
	// Stored git discovery key formats:
	// - "tag+commit" (preferred)
	// - "commit" (commit-only installs)
	if idx := strings.Index(version, "+"); idx > 0 && idx < len(version)-1 {
		return strings.TrimSpace(version[:idx]), strings.TrimSpace(version[idx+1:])
	}
	return "", version
}

func (ls *ListService) gitDiscoveredRefsFromDB(sourceID string) ([]gitDiscoveredRef, error) {
	vers, err := providers.ListDiscoveredVersions(sourceID)
	if err != nil {
		return nil, err
	}
	out := make([]gitDiscoveredRef, 0, len(vers))
	for _, dv := range vers {
		ref, commit := parseGitDiscoveryKeyVersion(dv.Version)
		if commit == "" {
			continue
		}
		out = append(out, gitDiscoveredRef{
			Ref:       ref,
			Commit:    commit,
			FirstSeen: dv.FirstSeen,
		})
	}
	return out, nil
}

// formatDiscoveredVersion formats a discovered version for display with local first-seen age.
func formatDiscoveredVersion(display string, firstSeen, now time.Time) string {
	display = strings.TrimSpace(display)
	if display == "" {
		return display
	}
	if firstSeen.IsZero() {
		return display
	}
	return fmt.Sprintf("%s (%s)", display, formatDaysAgo(now.Sub(firstSeen)))
}

// formatGitRefWithCommitAge formats "main (322c79d) (0 days ago)" for Installed/Available.
func formatGitRefWithCommitAge(ref, commit string, firstSeen, now time.Time) string {
	ref = strings.TrimSpace(ref)
	sha := shortGitSHA(commit)
	var label string
	switch {
	case ref != "" && sha != "":
		label = fmt.Sprintf("%s (%s)", ref, sha)
	case sha != "":
		label = sha
	default:
		label = ref
	}
	return formatDiscoveredVersion(label, firstSeen, now)
}

// formatPreferBranchDiscovered formats Discovered when a stale tag was superseded by a branch tip.
// Example: "322c79d (0 days ago; v1.2.3 120 days ago)"
func formatPreferBranchDiscovered(shortSHA string, firstSeen, now time.Time, tag string, tagAge time.Duration) string {
	shortSHA = strings.TrimSpace(shortSHA)
	tag = strings.TrimSpace(tag)
	base := formatDiscoveredVersion(shortSHA, firstSeen, now)
	if tag == "" {
		return base
	}
	tagNote := fmt.Sprintf("%s %s", tag, formatDaysAgo(tagAge))
	if firstSeen.IsZero() || !strings.HasSuffix(base, ")") {
		return fmt.Sprintf("%s (%s)", shortSHA, tagNote)
	}
	return strings.TrimSuffix(base, ")") + "; " + tagNote + ")"
}

// formatEligibleGitRef formats Eligible / EligibleSoon for git refs.
// Eligible: "main (322c79d)"; soon: "main (322c79d) in 7 days".
func formatEligibleGitRef(ref, commit string, remaining time.Duration) string {
	ref = strings.TrimSpace(ref)
	sha := shortGitSHA(commit)
	if ref == "" {
		ref = sha
		sha = ""
	}
	base := ref
	if sha != "" {
		base = fmt.Sprintf("%s (%s)", ref, sha)
	}
	return formatEligibleVersion(base, remaining)
}

// formatEligibleVersion formats Eligible / EligibleSoon for non-git versions.
// Eligible: "4.11.0"; soon: "4.11.0 in 7 days".
func formatEligibleVersion(version string, remaining time.Duration) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return version
	}
	if remaining > 0 {
		return fmt.Sprintf("%s in %s", version, formatInDuration(remaining))
	}
	return version
}

func shortGitSHA(commit string) string {
	commit = strings.TrimSpace(commit)
	if len(commit) >= 7 {
		return commit[:7]
	}
	return commit
}

func isPreferBranchRef(ref string) bool {
	return providers.IsPreferBranchRef(ref)
}

func commitMatchesRef(fullCommit, avail string) bool {
	fullCommit = strings.ToLower(strings.TrimSpace(fullCommit))
	avail = strings.ToLower(strings.TrimSpace(avail))
	if fullCommit == "" || avail == "" {
		return false
	}
	return fullCommit == avail || strings.HasPrefix(fullCommit, avail)
}

// formatInstalledGitDisplay formats the Installed column for git-hosted packages.
// Example: "main (322c79c) (1 day ago)"
func formatInstalledGitDisplay(sourceID, version, commit string, now time.Time) string {
	version = strings.TrimSpace(version)
	commit = strings.TrimSpace(commit)
	if version == "" {
		return "-"
	}
	var firstSeen time.Time
	if commit != "" {
		key := providers.FormatGitDiscoveryVersionForRef(version, commit)
		if t, ok, err := providers.GetFirstSeen(sourceID, key); err == nil && ok {
			firstSeen = t
		}
	}
	if commit != "" && (isPreferBranchRef(version) || providers.IsGitCommitHash(version) || providers.IsGitHostedSourceID(sourceID)) {
		return formatGitRefWithCommitAge(version, commit, firstSeen, now)
	}
	return formatDiscoveredVersion(version, firstSeen, now)
}

func (ls *ListService) discoveryDisplayForInstalled(sourceID, installedVersion, installedCommit string) discoveryDisplay {
	stable, prerelease := ls.registry.GetLatestVersions(sourceID)
	var remoteLatest providers.RemoteLatestEntry
	var hasRemoteLatest bool
	if stable == "" && prerelease == "" {
		if entry, ok, err := providers.GetRemoteLatest(sourceID); err == nil && ok {
			stable = entry.Version
			remoteLatest = entry
			hasRemoteLatest = true
		}
	}

	// Non-git / preliminary available labels (git path may enrich these below).
	avail := make([]string, 0, 2)
	if v := strings.TrimSpace(stable); v != "" && v != "latest" {
		if providers.IsGitCommitHash(v) {
			avail = append(avail, v[:7])
		} else {
			avail = append(avail, v)
		}
	}
	if v := strings.TrimSpace(prerelease); v != "" && v != "latest" && v != stable {
		if providers.IsGitCommitHash(v) {
			avail = append(avail, v[:7])
		} else {
			avail = append(avail, v)
		}
	}

	// Git-hosted packages use discovery keys that include the commit SHA ("tag+commit" or "commit").
	// The local installed version usually does *not* include the commit, so we can't reliably
	// filter/match by semver against the raw discovered keys. Instead, resolve each available tag
	// to its discovery key and then check first-seen directly.
	if providers.IsGitHostedSourceID(sourceID) {
		if item := ls.getRegistryItem(sourceID); item.Git != nil && len(item.Git.Refs) > 0 {
			return ls.discoveryDisplayFromRegistryGit(sourceID, installedVersion, installedCommit, item)
		}
		// Prefer policy-resolved remote_latest over the registry's bare semver tag when git.refs
		// are not yet published (e.g. main chosen over stale v1.10.2). Competing semver tags in
		// the cache (e.g. v1.0.0 vs registry v0.25.0) must not override the registry.
		if entry, ok, err := providers.GetRemoteLatest(sourceID); err == nil && ok {
			if providers.PreferRemoteLatestOverRegistry(entry, stable, prerelease) {
				return ls.discoveryDisplayForRemoteLatestGit(sourceID, installedVersion, installedCommit, entry)
			}
		}
		now := time.Now()
		minAge := cfg.Flags.MinReleaseAge
		installedVersion = strings.TrimSpace(installedVersion)
		installedCommit = strings.TrimSpace(installedCommit)

		matchRefs := make([]string, 0, 2)
		if v := strings.TrimSpace(stable); v != "" && v != "latest" {
			matchRefs = append(matchRefs, v)
		}
		if v := strings.TrimSpace(prerelease); v != "" && v != "latest" && v != stable {
			matchRefs = append(matchRefs, v)
		}

		out := discoveryDisplay{}

		// IMPORTANT: listing must not trigger network work. For git providers, do NOT
		// resolve tags to commits here. Instead, derive discovery state purely from
		// what's already recorded in discovery.json (tag+commit / commit).
		refs, err := ls.gitDiscoveredRefsFromDB(sourceID)
		if err != nil {
			out.Available = avail
			return out
		}

		// For each available ref (tag or commit), see if we have any discovery entry for it.
		// - For tags: any recorded "tag+<commit>" counts as discovered; we use the newest firstSeen.
		// - For commit-only refs: a recorded "<commit>" counts as discovered.
		for _, v := range matchRefs {
			v = strings.TrimSpace(v)
			if v == "" || v == "latest" {
				continue
			}

			var (
				found          bool
				foundCommit    string
				foundFirstSeen time.Time
			)
			for _, r := range refs {
				if r.Ref != "" {
					// tag+commit or branch+commit entry
					if r.Ref == v {
						if !found || r.FirstSeen.After(foundFirstSeen) {
							found = true
							foundCommit = r.Commit
							foundFirstSeen = r.FirstSeen
						}
					}
					continue
				}
				// commit-only entry
				if commitMatchesRef(r.Commit, v) {
					if !found || r.FirstSeen.After(foundFirstSeen) {
						found = true
						foundCommit = r.Commit
						foundFirstSeen = r.FirstSeen
					}
				}
			}

			// Prefer remote_latest commit when this is the cached latest ref.
			displayCommit := foundCommit
			if hasRemoteLatest && strings.EqualFold(strings.TrimSpace(remoteLatest.Version), v) && strings.TrimSpace(remoteLatest.Commit) != "" {
				displayCommit = remoteLatest.Commit
				if !found {
					// Still show Available from remote_latest even before first-seen is recorded.
					foundFirstSeen = time.Time{}
				}
			}

			// Available: "main (322c79d) (0 days ago)" for branch tips with a commit;
			// otherwise keep the bare ref/tag label.
			availLabel := v
			if providers.IsGitCommitHash(availLabel) {
				availLabel = availLabel[:7]
			}
			if displayCommit != "" && (isPreferBranchRef(v) || providers.IsGitCommitHash(v)) {
				availLabel = formatGitRefWithCommitAge(v, displayCommit, foundFirstSeen, now)
			} else if found && !foundFirstSeen.IsZero() {
				availLabel = formatDiscoveredVersion(availLabel, foundFirstSeen, now)
			}
			out.Available = append(out.Available, availLabel)

			if !found {
				continue
			}

			// Discovered: tags keep the tag name; branches/commits show short SHA.
			// When prefer-branch superseded a stale tag: "322c79d (0 days ago; v1.2.3 120 days ago)".
			discoveredLabel := v
			if providers.IsGitCommitHash(discoveredLabel) {
				discoveredLabel = discoveredLabel[:7]
			}
			if isPreferBranchRef(v) || providers.IsGitCommitHash(v) {
				if foundCommit != "" {
					discoveredLabel = shortGitSHA(foundCommit)
				}
			}
			if hasRemoteLatest && remoteLatest.HasSupersededTag() && isPreferBranchRef(v) {
				tagAge := time.Duration(0)
				if remoteLatest.SupersededUnix > 0 {
					tagAge = now.Sub(time.Unix(remoteLatest.SupersededUnix, 0))
				}
				discoveredLabel = formatPreferBranchDiscovered(discoveredLabel, foundFirstSeen, now, remoteLatest.SupersededTag, tagAge)
			} else {
				discoveredLabel = formatDiscoveredVersion(discoveredLabel, foundFirstSeen, now)
			}
			out.Discovered = append(out.Discovered, discoveredLabel)

			// If the user already has this exact ref pinned (same tag + same commit),
			// it is not an install candidate; don't show it as eligible.
			if installedVersion != "" && v == installedVersion && installedCommit != "" && foundCommit != "" && strings.EqualFold(installedCommit, foundCommit) {
				continue
			}

			age := now.Sub(foundFirstSeen)
			eligibleLabel := formatEligibleGitRef(v, foundCommit, 0)
			if minAge <= 0 || age >= minAge {
				out.Eligible = append(out.Eligible, eligibleLabel)
			} else {
				out.EligibleSoon = append(out.EligibleSoon, formatEligibleGitRef(v, foundCommit, minAge-age))
			}
		}

		if len(out.Available) == 0 {
			out.Available = avail
		}
		return out
	}

	now := time.Now()
	minAge := cfg.Flags.MinReleaseAge
	out := discoveryDisplay{Available: avail}
	eligibleSeen := map[string]struct{}{}

	// Registry candidates newer than installed: lazy-record first-seen (like git) and
	// fill Eligible / EligibleSoon so Available can show "in X days" even when discovery.json
	// was empty (warm registry cache).
	candidates := make([]string, 0, 2)
	if v := strings.TrimSpace(stable); v != "" && v != "latest" {
		candidates = append(candidates, v)
	}
	if v := strings.TrimSpace(prerelease); v != "" && v != "latest" && v != strings.TrimSpace(stable) {
		candidates = append(candidates, v)
	}
	for _, v := range candidates {
		if !shouldRecordDiscoveredVersion(installedVersion, v) {
			continue
		}
		firstSeen, found, err := providers.GetFirstSeen(sourceID, v)
		if err != nil || !found {
			_ = providers.RecordDiscoveryBatch([]providers.DiscoveryPair{{
				SourceID: sourceID,
				Version:  v,
			}})
			if t, seen, getErr := providers.GetFirstSeen(sourceID, v); getErr == nil && seen {
				firstSeen = t
			} else {
				firstSeen = now
			}
		}
		displayVersion := v
		if providers.IsGitCommitHash(displayVersion) {
			displayVersion = displayVersion[:7]
		}
		age := now.Sub(firstSeen)
		if minAge <= 0 || age >= minAge {
			out.Eligible = append(out.Eligible, formatEligibleVersion(displayVersion, 0))
		} else {
			out.EligibleSoon = append(out.EligibleSoon, formatEligibleVersion(displayVersion, minAge-age))
		}
		eligibleSeen[v] = struct{}{}
	}

	discovered, err := providers.ListDiscoveredVersions(sourceID)
	if err != nil {
		return out
	}
	for _, dv := range discovered {
		version := dv.Version
		displayVersion := version
		if providers.IsGitCommitHash(displayVersion) {
			displayVersion = version[:7]
		}
		// Always show what's been recorded, even if it's not newer than installed.
		out.Discovered = append(out.Discovered, formatDiscoveredVersion(displayVersion, dv.FirstSeen, now))

		// Eligibility for versions already in discovery but not covered by registry candidates.
		if !shouldRecordDiscoveredVersion(installedVersion, dv.Version) {
			continue
		}
		if _, ok := eligibleSeen[dv.Version]; ok {
			continue
		}
		age := now.Sub(dv.FirstSeen)
		if minAge <= 0 || age >= minAge {
			out.Eligible = append(out.Eligible, formatEligibleVersion(displayVersion, 0))
		} else {
			out.EligibleSoon = append(out.EligibleSoon, formatEligibleVersion(displayVersion, minAge-age))
		}
		eligibleSeen[dv.Version] = struct{}{}
	}
	return out
}

func (ls *ListService) discoveryDisplayFromRegistryGit(sourceID, installedVersion, installedCommit string, item registry_parser.RegistryItem) discoveryDisplay {
	result, _, ok := providers.ResolveGitLatestFromRegistry(item, providers.PreferBranchPolicyForSourceID(sourceID))
	if !ok {
		return discoveryDisplay{}
	}
	ref := strings.TrimSpace(result.Version)
	commit := strings.TrimSpace(result.Commit)
	if ref == "" {
		return discoveryDisplay{}
	}
	var supersededTag string
	var supersededUnix int64
	if result.SupersededTag != "" {
		supersededTag = result.SupersededTag
		supersededUnix = result.SupersededTimeUnix
	}
	return ls.discoveryDisplayForResolvedGitRef(sourceID, installedVersion, installedCommit, ref, commit, supersededTag, supersededUnix)
}

func (ls *ListService) discoveryDisplayForRemoteLatestGit(sourceID, installedVersion, installedCommit string, entry providers.RemoteLatestEntry) discoveryDisplay {
	return ls.discoveryDisplayForResolvedGitRef(
		sourceID, installedVersion, installedCommit,
		entry.Version, entry.Commit,
		entry.SupersededTag, entry.SupersededUnix,
	)
}

func (ls *ListService) discoveryDisplayForResolvedGitRef(sourceID, installedVersion, installedCommit, ref, commit, supersededTag string, supersededUnix int64) discoveryDisplay {
	now := time.Now()
	minAge := cfg.Flags.MinReleaseAge
	installedVersion = strings.TrimSpace(installedVersion)
	installedCommit = strings.TrimSpace(installedCommit)
	ref = strings.TrimSpace(ref)
	commit = strings.TrimSpace(commit)

	out := discoveryDisplay{}
	var firstSeen time.Time
	if commit != "" {
		key := providers.FormatGitDiscoveryVersionForRef(ref, commit)
		if t, seen, err := providers.GetFirstSeen(sourceID, key); err == nil && seen {
			firstSeen = t
		}
	}

	sameAsInstalled := installedVersion != "" && ref == installedVersion && installedCommit != "" && commit != "" && strings.EqualFold(installedCommit, commit)
	isUpdate := !sameAsInstalled && (shouldRecordDiscoveredVersion(installedVersion, ref) || providers.HasGitCommitUpdate(installedCommit, commit) || (installedVersion != "" && !strings.EqualFold(installedVersion, ref)))

	// Start the local discovery clock the first time we surface a newer git tip from
	// registry/remote_latest, so Available can show "in X days" under min-release-age.
	if firstSeen.IsZero() && isUpdate && commit != "" {
		_ = providers.RecordDiscoveryBatch([]providers.DiscoveryPair{{
			SourceID: sourceID,
			Version:  ref,
			Commit:   commit,
		}})
		if t, seen, err := providers.GetFirstSeen(sourceID, providers.FormatGitDiscoveryVersionForRef(ref, commit)); err == nil && seen {
			firstSeen = t
		} else {
			firstSeen = now
		}
	}

	availLabel := ref
	if commit != "" && (isPreferBranchRef(ref) || providers.IsGitCommitHash(ref)) {
		availLabel = formatGitRefWithCommitAge(ref, commit, firstSeen, now)
	} else if !firstSeen.IsZero() {
		availLabel = formatDiscoveredVersion(availLabel, firstSeen, now)
	}
	out.Available = append(out.Available, availLabel)

	if firstSeen.IsZero() {
		// No discovery clock and nothing newer than installed.
		if isUpdate && minAge <= 0 {
			out.Eligible = append(out.Eligible, formatEligibleGitRef(ref, commit, 0))
		}
		return out
	}

	discoveredLabel := ref
	if isPreferBranchRef(ref) || providers.IsGitCommitHash(ref) {
		if commit != "" {
			discoveredLabel = shortGitSHA(commit)
		}
	}
	if strings.TrimSpace(supersededTag) != "" && isPreferBranchRef(ref) {
		tagAge := time.Duration(0)
		if supersededUnix > 0 {
			tagAge = now.Sub(time.Unix(supersededUnix, 0))
		}
		discoveredLabel = formatPreferBranchDiscovered(discoveredLabel, firstSeen, now, supersededTag, tagAge)
	} else {
		discoveredLabel = formatDiscoveredVersion(discoveredLabel, firstSeen, now)
	}
	out.Discovered = append(out.Discovered, discoveredLabel)

	if sameAsInstalled || !isUpdate {
		return out
	}

	age := now.Sub(firstSeen)
	eligibleLabel := formatEligibleGitRef(ref, commit, 0)
	if minAge <= 0 || age >= minAge {
		out.Eligible = append(out.Eligible, eligibleLabel)
	} else {
		out.EligibleSoon = append(out.EligibleSoon, formatEligibleGitRef(ref, commit, minAge-age))
	}
	return out
}

func joinVersionsOrDash(v []string) string {
	if len(v) == 0 {
		return "-"
	}
	return strings.Join(v, ", ")
}

func (ls *ListService) discoveryPairsForRegistry(registry []registry_parser.RegistryItem) []providers.DiscoveryPair {
	if cfg.Flags.MinReleaseAge <= 0 || len(registry) == 0 {
		return nil
	}
	pairs := make([]providers.DiscoveryPair, 0, len(registry)*2)
	for _, it := range registry {
		id := strings.TrimSpace(it.Source.ID)
		if id == "" || providers.IsGitHostedSourceID(id) {
			// Skip git-hosted entries: resolving tag→commit for the full registry would
			// require thousands of ls-remote calls. Git discovery is recorded at install
			// time (and from the lockfile on nvpm list for installed packages).
			continue
		}
		if v := strings.TrimSpace(it.Version); v != "" && v != "latest" {
			pairs = append(pairs, providers.DiscoveryPair{SourceID: id, Version: v})
		}
		if v := strings.TrimSpace(it.PrereleaseVersion); v != "" && v != "latest" {
			pairs = append(pairs, providers.DiscoveryPair{SourceID: id, Version: v})
		}
	}
	return pairs
}

func (ls *ListService) recordDiscoveryOnRegistryRefresh(refreshed bool, buildPairs func() []providers.DiscoveryPair) {
	if !refreshed {
		return
	}
	pairs := buildPairs()
	if len(pairs) == 0 {
		return
	}
	_ = providers.RecordDiscoveryBatch(pairs)
}

func (ls *ListService) packageInRegistry(sourceID string) bool {
	stable, prerelease := ls.registry.GetLatestVersions(sourceID)
	return strings.TrimSpace(stable) != "" || strings.TrimSpace(prerelease) != ""
}

func (ls *ListService) discoverNonRegistryInstalled(localPackages []local_packages_parser.LocalPackageItem) {
	showProgress := showDiscoveryProgress && !ShouldUseJSONOutput()
	_ = providers.DiscoverNonRegistryGitPackages(localPackages, ls.packageInRegistry, showProgress)
}

func (ls *ListService) recordRegistryDiscoveriesAfterRefresh(refreshed bool, localPackages []local_packages_parser.LocalPackageItem) {
	ls.recordDiscoveryOnRegistryRefresh(refreshed, func() []providers.DiscoveryPair {
		return ls.discoveryPairsForInstalled(localPackages)
	})
}

// ListInstalledPackages lists locally installed packages.
// Name filters (opts.NameFilters) match IDs, names, or registry aliases (substring, case-insensitive).
// Optional opts.OnlyOutdated, OnlyProviders, and OnlyCategories are applied in addition (AND).
func (ls *ListService) ListInstalledPackages(opts ListQueryOptions) {
	var localPackages []local_packages_parser.LocalPackageItem
	var refreshed bool
	filters := opts.NameFilters

	// Download uses its own top-level spinner (clears when finished) so it does not
	// leave a permanent line and is not nested under a prep spinner.
	refreshed, _ = ls.fileDownloader.DownloadAndUnzipRegistry()
	localPackages = ls.localPackages.GetData(true).Packages
	ls.registry.GetData(refreshed)
	if refreshed {
		if ls.shouldShowListPrepSpinner() {
			_ = spinnerutil.Run("Releases discovery (min-age) ...", func() {
				ls.recordRegistryDiscoveriesAfterRefresh(true, localPackages)
			})
		} else {
			ls.recordRegistryDiscoveriesAfterRefresh(true, localPackages)
		}
		// Per-package remotes also use top-level spinners after download has cleared.
		ls.discoverNonRegistryInstalled(localPackages)
	}

	// Truncate git commit hashes to 7 characters for display
	for idx, pkg := range localPackages {
		if providers.IsGitCommitHash(pkg.Version) {
			localPackages[idx].Version = pkg.Version[:7]
		}
	}

	// Filter packages if name filters are provided
	filteredPackages := localPackages
	if len(filters) > 0 {
		filteredPackages = []local_packages_parser.LocalPackageItem{}
		parser := newRegistryParser()
		for _, pkg := range localPackages {
			packageName := getPackageNameFromSourceID(pkg.SourceID)
			packageNameLower := strings.ToLower(packageName)
			sourceIDLower := strings.ToLower(pkg.SourceID)

			// Check if package name, full sourceID, or aliases contain any of the filter strings
			matches := false
			for _, filter := range filters {
				filterLower := strings.ToLower(filter)
				// Match against full sourceID (provider:package-id) or just package name
				if strings.Contains(sourceIDLower, filterLower) || strings.Contains(packageNameLower, filterLower) {
					matches = true
					break
				}

				// Also check aliases from registry
				registryItem := parser.GetBySourceId(pkg.SourceID)
				if registryItem.Source.ID != "" {
					for _, alias := range registryItem.Aliases {
						aliasLower := strings.ToLower(alias)
						if strings.Contains(aliasLower, filterLower) {
							matches = true
							break
						}
					}
					if matches {
						break
					}
				}
			}

			if matches {
				filteredPackages = append(filteredPackages, pkg)
			}
		}
	}

	filteredPackages = ls.applyAdvancedFiltersToInstalled(filteredPackages, opts)

	// Output based on mode
	if ShouldUseJSONOutput() {
		ls.listInstalledPackagesJSON(filteredPackages, opts)
	} else if ShouldUsePlainOutput() {
		ls.listInstalledPackagesPlain(filteredPackages, opts)
	} else {
		ls.listInstalledPackagesRich(filteredPackages, opts)
	}
}

func (ls *ListService) applyAdvancedFiltersToInstalled(packages []local_packages_parser.LocalPackageItem, opts ListQueryOptions) []local_packages_parser.LocalPackageItem {
	if !opts.hasAdvancedFilters() {
		return packages
	}
	catByID := ls.registryCategoriesBySourceID()
	var itemsByID map[string]registry_parser.RegistryItem
	if len(opts.ShowFilters) > 0 {
		itemsByID = make(map[string]registry_parser.RegistryItem)
		for _, it := range ls.registry.GetData(false) {
			id := strings.TrimSpace(it.Source.ID)
			if id != "" {
				itemsByID[id] = it
			}
		}
	}
	out := make([]local_packages_parser.LocalPackageItem, 0, len(packages))
	for _, pkg := range packages {
		prov := getProviderFromSourceID(pkg.SourceID)
		if len(opts.OnlyProviders) > 0 && !slices.Contains(opts.OnlyProviders, prov) {
			continue
		}
		if len(opts.OnlyCategories) > 0 {
			cats := catByID[pkg.SourceID]
			if !registryItemMatchesCategoryFilters(cats, opts.OnlyCategories) {
				continue
			}
		}
		if opts.OnlyOutdated {
			if _, hasUpdate := ls.checkUpdateAvailability(pkg.SourceID, pkg.Version, pkg.Commit); !hasUpdate {
				continue
			}
		}
		if opts.OnlyPlugins {
			if pkg.Extras == nil || pkg.Extras.Kind != local_packages_parser.KindNeovimPlugin {
				continue
			}
		}
		if opts.OnlyAlwaysTrusted {
			if pkg.Extras == nil || !pkg.Extras.AlwaysTrust {
				continue
			}
		}
		if len(opts.ShowFilters) > 0 {
			if item, ok := itemsByID[pkg.SourceID]; ok {
				if !MatchShowJSON(buildPackageInfoJSON(item, pkg.SourceID), opts.ShowFilters) {
					continue
				}
			} else if !packageMatchesShowFilters(pkg.SourceID, opts.ShowFilters) {
				continue
			}
		}
		out = append(out, pkg)
	}
	return out
}

func (ls *ListService) registryCategoriesBySourceID() map[string][]string {
	items := ls.registry.GetData(false)
	m := make(map[string][]string, len(items))
	for _, it := range items {
		id := strings.TrimSpace(it.Source.ID)
		if id == "" {
			continue
		}
		m[id] = it.Categories
	}
	return m
}

// listInstalledPackagesRich lists installed packages with rich formatting using markdown tables
func (ls *ListService) listInstalledPackagesRich(filteredPackages []local_packages_parser.LocalPackageItem, opts ListQueryOptions) {
	var markdown strings.Builder
	filters := opts.NameFilters

	markdown.WriteString(fmt.Sprintf("# %s Locally Installed Packages\n\n", IconSummaryPlain()))

	if len(filteredPackages) == 0 {
		if len(filters) > 0 || opts.hasAdvancedFilters() {
			markdown.WriteString("No installed packages match the current criteria")
			if len(filters) > 0 {
				markdown.WriteString(fmt.Sprintf(" (name filters: %s)", strings.Join(filters, ", ")))
			}
			markdown.WriteString(opts.constraintDescriptionMarkdown())
			markdown.WriteString(".\n")
		} else {
			markdown.WriteString("No packages are currently installed.\n\n")
			markdown.WriteString("Use `nvpm install <pkgId>` to install packages.\n")
		}
		ls.renderMarkdown(markdown.String())
		return
	}

	markdown.WriteString(fmt.Sprintf("Found **%d** installed packages", len(filteredPackages)))
	if len(filters) > 0 {
		markdown.WriteString(fmt.Sprintf(" matching name filters: %s", strings.Join(filters, ", ")))
	}
	markdown.WriteString(opts.constraintDescriptionMarkdown())
	markdown.WriteString("\n\n")

	// Group packages by provider
	packagesByProvider := make(map[string][]local_packages_parser.LocalPackageItem)
	for _, pkg := range filteredPackages {
		provider := getProviderFromSourceID(pkg.SourceID)
		packagesByProvider[provider] = append(packagesByProvider[provider], pkg)
	}

	// Display packages grouped by provider and count updates
	providerOrder := []string{"npm", "golang", "pypi", "cargo", "github", "gitlab", "codeberg", "gem", "composer", "luarocks", "nuget", "opam", "openvsx", "generic"}
	updateCount := 0
	totalCount := 0

	for _, provider := range providerOrder {
		if packages, exists := packagesByProvider[provider]; exists {
			markdown.WriteString(fmt.Sprintf("## %s Packages\n\n", strings.ToUpper(provider)))
			markdown.WriteString("| Package ID | Installed | Available |\n")
			markdown.WriteString("|------------|-----------|----------|\n")

			for _, pkg := range packages {
				updateInfo, hasUpdate := ls.checkUpdateAvailability(pkg.SourceID, pkg.Version, pkg.Commit)
				// Clean up update info for table display (remove icons, keep text)
				statusText := strings.ReplaceAll(updateInfo, IconRefresh(), "")
				statusText = strings.ReplaceAll(statusText, IconCheckCircle(), "")
				statusText = strings.TrimSpace(statusText)
				if hasUpdate {
					if statusText == "" {
						statusText = "Update available"
					}
					// Make updates pop in markdown (icon + bold)
					statusText = fmt.Sprintf("%s **%s**", IconRefreshPlain(), statusText)
				} else {
					if statusText == "" {
						statusText = "Up to date"
					}
				}

				disc := ls.discoveryDisplayForInstalled(pkg.SourceID, pkg.Version, pkg.Commit)
				availableText := joinVersionsOrDash(mergedAvailableColumn(disc))
				if hasUpdate && statusText != "" {
					// include update summary first, then discovery details in separate columns
					_ = statusText
				}

				installedText := pkg.Version
				if providers.IsGitHostedSourceID(pkg.SourceID) {
					installedText = formatInstalledGitDisplay(pkg.SourceID, pkg.Version, pkg.Commit, time.Now())
				}

				markdown.WriteString(fmt.Sprintf("| %s | %s | %s |\n",
					pkg.SourceID,
					installedText,
					availableText,
				))

				totalCount++
				if hasUpdate {
					updateCount++
				}
			}
			markdown.WriteString("\n")
		}
	}

	// Show summary
	markdown.WriteString("### Summary\n\n")
	markdown.WriteString(fmt.Sprintf("- **%d** of **%d** packages are up to date", totalCount-updateCount, totalCount))
	if updateCount > 0 {
		markdown.WriteString(fmt.Sprintf("\n- **%d** updates available", updateCount))
		markdown.WriteString(fmt.Sprintf("\n- %s Use `nvpm update --all` to update all packages", IconLightbulbPlain()))
	}
	markdown.WriteString("\n")

	ls.renderMarkdown(markdown.String())
}

// listInstalledPackagesPlain lists installed packages in plain text format
func (ls *ListService) listInstalledPackagesPlain(filteredPackages []local_packages_parser.LocalPackageItem, opts ListQueryOptions) {
	filters := opts.NameFilters
	fmt.Printf("%s Locally Installed Packages\n\n", IconSummary())

	if len(filteredPackages) == 0 {
		if len(filters) > 0 || opts.hasAdvancedFilters() {
			fmt.Print("No installed packages match the current criteria")
			if len(filters) > 0 {
				fmt.Printf(" (name filters: %s)", strings.Join(filters, ", "))
			}
			fmt.Println(opts.constraintDescriptionPlain() + ".")
		} else {
			fmt.Println("No packages are currently installed.")
			fmt.Println("Use 'nvpm install <pkgId>' to install packages.")
		}
		return
	}

	fmt.Printf("Found %d installed packages", len(filteredPackages))
	if len(filters) > 0 {
		fmt.Printf(" matching name filters: %s", strings.Join(filters, ", "))
	}
	fmt.Print(opts.constraintDescriptionPlain())
	fmt.Printf(":\n\n")

	// Group packages by provider
	packagesByProvider := make(map[string][]local_packages_parser.LocalPackageItem)
	for _, pkg := range filteredPackages {
		provider := getProviderFromSourceID(pkg.SourceID)
		packagesByProvider[provider] = append(packagesByProvider[provider], pkg)
	}

	providerOrder := []string{"npm", "golang", "pypi", "cargo", "github", "gitlab", "codeberg", "gem", "composer", "luarocks", "nuget", "opam", "openvsx", "generic"}
	updateCount := 0
	totalCount := 0

	for _, provider := range providerOrder {
		if packages, exists := packagesByProvider[provider]; exists {
			fmt.Printf("%s %s Packages:\n", IconDiamond(), strings.ToUpper(provider))
			for _, pkg := range packages {
				updateInfo, hasUpdate := ls.checkUpdateAvailability(pkg.SourceID, pkg.Version, pkg.Commit)
				installedText := "v" + pkg.Version
				if providers.IsGitHostedSourceID(pkg.SourceID) {
					installedText = formatInstalledGitDisplay(pkg.SourceID, pkg.Version, pkg.Commit, time.Now())
				}
				fmt.Printf("   %s %s (%s) %s\n", getProviderIcon(provider), pkg.SourceID, installedText, updateInfo)
				disc := ls.discoveryDisplayForInstalled(pkg.SourceID, pkg.Version, pkg.Commit)
				if cfg.Flags.MinReleaseAge > 0 {
					if merged := mergedAvailableColumn(disc); len(merged) > 0 {
						fmt.Printf("      available:  %s\n", strings.Join(merged, ", "))
					}
				}
				totalCount++
				if hasUpdate {
					updateCount++
				}
			}
			fmt.Println()
		}
	}

	// Show summary
	fmt.Printf("%s Summary: %d of %d packages are up to date", IconSummary(), totalCount-updateCount, totalCount)
	if updateCount > 0 {
		fmt.Printf(", %d updates available", updateCount)
		fmt.Printf("\n%s Use 'nvpm update --all' to update all packages", IconLightbulb())
	}
	fmt.Println()
}

// listInstalledPackagesJSON lists installed packages in JSON format
func (ls *ListService) listInstalledPackagesJSON(filteredPackages []local_packages_parser.LocalPackageItem, opts ListQueryOptions) {
	filters := opts.NameFilters
	result := make(map[string]any)
	result["type"] = "installed"
	if len(filters) > 0 {
		result["filters"] = filters
	}
	appendListQueryJSONFields(result, opts)

	if len(filteredPackages) == 0 {
		result["count"] = 0
		result["packages"] = []any{}
		PrintJSON(result)
		return
	}

	packagesData := make([]map[string]any, 0, len(filteredPackages))
	updateCount := 0

	for _, pkg := range filteredPackages {
		packageName := getPackageNameFromSourceID(pkg.SourceID)
		provider := getProviderFromSourceID(pkg.SourceID)
		_, hasUpdate := ls.checkUpdateAvailability(pkg.SourceID, pkg.Version, pkg.Commit)
		disc := ls.discoveryDisplayForInstalled(pkg.SourceID, pkg.Version, pkg.Commit)

		pkgData := map[string]any{
			"source_id":           pkg.SourceID,
			"name":                packageName,
			"provider":            provider,
			"version":             pkg.Version,
			"has_update":          hasUpdate,
			"available_versions":  disc.Available,
			"discovered_versions": disc.Discovered,
			"eligible_versions":   disc.Eligible,
		}
		if providers.IsGitHostedSourceID(pkg.SourceID) {
			pkgData["installed_display"] = formatInstalledGitDisplay(pkg.SourceID, pkg.Version, pkg.Commit, time.Now())
			if c := strings.TrimSpace(pkg.Commit); c != "" {
				pkgData["commit"] = c
			}
		}
		if len(disc.EligibleSoon) > 0 {
			pkgData["eligible_soon_versions"] = disc.EligibleSoon
		}
		packagesData = append(packagesData, pkgData)

		if hasUpdate {
			updateCount++
		}
	}

	result["count"] = len(filteredPackages)
	result["packages"] = packagesData
	result["updates_available"] = updateCount
	PrintJSON(result)
}

// ListAllPackages lists all available packages from the registry.
// Name filters (opts.NameFilters) match IDs, names, or aliases (substring, case-insensitive).
// Optional opts.OnlyOutdated, OnlyProviders, and OnlyCategories apply in addition (AND).
func (ls *ListService) ListAllPackages(opts ListQueryOptions) {
	var registry []registry_parser.RegistryItem
	filters := opts.NameFilters

	refreshed, _ := ls.fileDownloader.DownloadAndUnzipRegistry()
	registry = ls.registry.GetData(refreshed)
	if refreshed {
		record := func() {
			ls.recordDiscoveryOnRegistryRefresh(true, func() []providers.DiscoveryPair {
				return ls.discoveryPairsForRegistry(registry)
			})
		}
		if ls.shouldShowListPrepSpinner() {
			_ = spinnerutil.Run("Releases discovery (min-age) ...", record)
		} else {
			record()
		}
	}

	if len(registry) == 0 {
		if !ShouldUseJSONOutput() {
			fmt.Println("No packages found in the registry.")
		}

		// Try to download the registry (spinner clears when done).
		retriedRefreshed, err := ls.fileDownloader.DownloadAndUnzipRegistry()
		if err != nil {
			if ShouldUseJSONOutput() {
				result := map[string]any{
					"type":    "all",
					"error":   "failed to download registry",
					"details": err.Error(),
				}
				PrintJSON(result)
			} else if ShouldUsePlainOutput() {
				fmt.Printf("[✗] Failed to download registry: %v\n", err)
				fmt.Println("[*] Use 'nvpm' (without flags) to download the registry manually.")
			} else {
				fmt.Printf("%s Failed to download registry: %v\n", IconCancel(), err)
				fmt.Printf("%s Use 'nvpm' (without flags) to download the registry manually.\n", IconLightbulb())
			}
			return
		}

		if !ShouldUseJSONOutput() {
			if ShouldUsePlainOutput() {
				fmt.Println("[✓] Registry downloaded successfully!")
				fmt.Println()
			} else {
				fmt.Printf("%s Registry downloaded successfully!\n", IconCheckCircle())
				fmt.Println()
			}
		}

		// Try to get the registry data again
		registry = ls.registry.GetData(true)
		ls.recordDiscoveryOnRegistryRefresh(retriedRefreshed, func() []providers.DiscoveryPair {
			return ls.discoveryPairsForRegistry(registry)
		})

		if len(registry) == 0 {
			if ShouldUseJSONOutput() {
				result := map[string]any{
					"type":  "all",
					"error": "still no packages found after downloading registry",
				}
				PrintJSON(result)
			} else if ShouldUsePlainOutput() {
				fmt.Println("[✗] Still no packages found after downloading registry.")
			} else {
				fmt.Printf("%s Still no packages found after downloading registry.\n", IconCancel())
			}
			return
		}
	}

	// Filter packages if filters are provided
	filteredRegistry := registry
	if len(filters) > 0 {
		filteredRegistry = []registry_parser.RegistryItem{}
		for _, pkg := range registry {
			packageName := getPackageNameFromSourceID(pkg.Source.ID)
			packageNameLower := strings.ToLower(packageName)
			sourceIDLower := strings.ToLower(pkg.Source.ID)

			// Check if package name, full sourceID, or aliases contain any of the filter strings
			matches := false
			for _, filter := range filters {
				filterLower := strings.ToLower(filter)
				// Match against full sourceID (provider:package-id) or just package name
				if strings.Contains(sourceIDLower, filterLower) || strings.Contains(packageNameLower, filterLower) {
					matches = true
					break
				}

				// Also check aliases
				for _, alias := range pkg.Aliases {
					aliasLower := strings.ToLower(alias)
					if strings.Contains(aliasLower, filterLower) {
						matches = true
						break
					}
				}
				if matches {
					break
				}
			}

			if matches {
				filteredRegistry = append(filteredRegistry, pkg)
			}
		}
	}

	filteredRegistry = ls.applyAdvancedFiltersToRegistry(filteredRegistry, opts)

	// Output based on mode
	if ShouldUseJSONOutput() {
		ls.listAllPackagesJSON(filteredRegistry, opts)
	} else if ShouldUsePlainOutput() {
		ls.listAllPackagesPlain(filteredRegistry, opts)
	} else {
		ls.listAllPackagesRich(filteredRegistry, opts)
	}
}

func (ls *ListService) applyAdvancedFiltersToRegistry(items []registry_parser.RegistryItem, opts ListQueryOptions) []registry_parser.RegistryItem {
	if !opts.hasAdvancedFilters() {
		return items
	}
	installedPackages := ls.localPackages.GetData(false).Packages
	installedMap := make(map[string]string, len(installedPackages))
	for _, pkg := range installedPackages {
		installedMap[pkg.SourceID] = pkg.Version
	}

	out := make([]registry_parser.RegistryItem, 0, len(items))
	for _, item := range items {
		id := item.Source.ID
		prov := getProviderFromSourceID(id)
		if len(opts.OnlyProviders) > 0 && !slices.Contains(opts.OnlyProviders, prov) {
			continue
		}
		if len(opts.OnlyCategories) > 0 {
			if !registryItemMatchesCategoryFilters(item.Categories, opts.OnlyCategories) {
				continue
			}
		}
		if opts.OnlyOutdated {
			installedVer, ok := installedMap[id]
			if !ok {
				continue
			}
			if _, hasUpdate := ls.checkUpdateAvailability(id, installedVer, ""); !hasUpdate {
				continue
			}
		}
		if opts.OnlyAlwaysTrusted {
			trusted := false
			for _, pkg := range installedPackages {
				if pkg.SourceID == id && pkg.Extras != nil && pkg.Extras.AlwaysTrust {
					trusted = true
					break
				}
			}
			if !trusted {
				continue
			}
		}
		if len(opts.ShowFilters) > 0 && !registryItemMatchesShowFilters(item, opts.ShowFilters) {
			continue
		}
		out = append(out, item)
	}
	return out
}

// listAllPackagesRich lists all packages with rich formatting using markdown tables
func (ls *ListService) listAllPackagesRich(filteredRegistry []registry_parser.RegistryItem, opts ListQueryOptions) {
	var markdown strings.Builder
	filters := opts.NameFilters

	markdown.WriteString(fmt.Sprintf("## %s All Available Packages\n\n", IconBookPlain()))

	if len(filteredRegistry) == 0 {
		if len(filters) > 0 || opts.hasAdvancedFilters() {
			markdown.WriteString("No packages match the current criteria")
			if len(filters) > 0 {
				markdown.WriteString(fmt.Sprintf(" (name filters: %s)", strings.Join(filters, ", ")))
			}
			markdown.WriteString(opts.constraintDescriptionMarkdown())
			markdown.WriteString(".\n")
		} else {
			markdown.WriteString("No packages found in the registry.\n")
		}
		ls.renderMarkdown(markdown.String())
		return
	}

	markdown.WriteString(fmt.Sprintf("Found **%d** packages in the registry", len(filteredRegistry)))
	if len(filters) > 0 {
		markdown.WriteString(fmt.Sprintf(" matching name filters: %s", strings.Join(filters, ", ")))
	}
	markdown.WriteString(opts.constraintDescriptionMarkdown())
	markdown.WriteString("\n\n")

	// Get installed packages to check status
	installedPackages := ls.localPackages.GetData(false).Packages
	installedMap := make(map[string]string) // sourceID -> version
	for _, pkg := range installedPackages {
		installedMap[pkg.SourceID] = pkg.Version
	}

	// Group packages by provider
	packagesByProvider := make(map[string][]registry_parser.RegistryItem)
	for _, pkg := range filteredRegistry {
		provider := getProviderFromSourceID(pkg.Source.ID)
		packagesByProvider[provider] = append(packagesByProvider[provider], pkg)
	}

	// Display packages grouped by provider
	providers := []string{"npm", "golang", "pypi", "cargo", "github", "gitlab", "codeberg", "gem", "composer", "luarocks", "nuget", "opam", "openvsx", "generic"}
	for _, provider := range providers {
		if packages, exists := packagesByProvider[provider]; exists {
			markdown.WriteString(fmt.Sprintf("### %s %s Packages (%d)\n\n", IconDiamondPlain(), strings.ToUpper(provider), len(packages)))
			markdown.WriteString("| Package ID | Version | Status | Description |\n")
			markdown.WriteString("|------------|---------|--------|-------------|\n")

			for _, pkg := range packages {
				installedVersion, isInstalled := installedMap[pkg.Source.ID]

				// Build status text
				statusText := ""
				if isInstalled {
					updateInfo, hasUpdate := ls.checkUpdateAvailability(pkg.Source.ID, installedVersion, "")
					if hasUpdate {
						// Clean up update info for table display
						statusText = strings.ReplaceAll(updateInfo, IconRefresh(), "")
						statusText = strings.TrimSpace(statusText)
						if statusText == "" {
							statusText = "Update available"
						}
						// Highlight updates in markdown (icon + bold)
						statusText = fmt.Sprintf("%s **%s**", IconRefreshPlain(), statusText)
					} else {
						statusText = fmt.Sprintf("%s Installed, up to date", IconCheckCirclePlain())
					}
				} else {
					statusText = fmt.Sprintf("%s Not installed", IconEmptyPlain())
				}

				// Escape pipe characters in description for markdown table
				description := pkg.Description
				if description != "" {
					description = strings.ReplaceAll(description, "|", "\\|")
				} else {
					description = "-"
				}

				markdown.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n", pkg.Source.ID, pkg.Version, statusText, description))
			}
			markdown.WriteString("\n")
		}
	}

	ls.renderMarkdown(markdown.String())
}

// renderMarkdown renders markdown content using glamour
func (ls *ListService) renderMarkdown(markdown string) {
	spinnerutil.ResetTerminal()

	// Get terminal width, default to 80 if not available
	width := 80
	if w, _, err := term.GetSize(os.Stdout.Fd()); err == nil && w > 0 {
		width = w
	}

	// Create a renderer with terminal width
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		// Fallback to plain render
		rendered, renderErr := glamour.Render(markdown, "dark")
		if renderErr != nil {
			fmt.Print(markdown)
			return
		}
		fmt.Print(rendered)
		return
	}

	rendered, err := r.Render(markdown)
	if err != nil {
		// Fallback to plain text if rendering fails
		fmt.Print(markdown)
		return
	}
	fmt.Print(rendered)
}

// listAllPackagesPlain lists all packages in plain text format
func (ls *ListService) listAllPackagesPlain(filteredRegistry []registry_parser.RegistryItem, opts ListQueryOptions) {
	filters := opts.NameFilters
	fmt.Printf("%s All Available Packages\n\n", IconBook())

	if len(filteredRegistry) == 0 {
		if len(filters) > 0 || opts.hasAdvancedFilters() {
			fmt.Print("No packages match the current criteria")
			if len(filters) > 0 {
				fmt.Printf(" (name filters: %s)", strings.Join(filters, ", "))
			}
			fmt.Println(opts.constraintDescriptionPlain() + ".")
		} else {
			fmt.Println("No packages found in the registry.")
		}
		return
	}

	fmt.Printf("Found %d packages in the registry", len(filteredRegistry))
	if len(filters) > 0 {
		fmt.Printf(" matching name filters: %s", strings.Join(filters, ", "))
	}
	fmt.Print(opts.constraintDescriptionPlain())
	fmt.Printf(":\n\n")

	// Get installed packages to check status
	installedPackages := ls.localPackages.GetData(false).Packages
	installedMap := make(map[string]string) // sourceID -> version
	for _, pkg := range installedPackages {
		installedMap[pkg.SourceID] = pkg.Version
	}

	// Group packages by provider
	packagesByProvider := make(map[string][]registry_parser.RegistryItem)
	for _, pkg := range filteredRegistry {
		provider := getProviderFromSourceID(pkg.Source.ID)
		packagesByProvider[provider] = append(packagesByProvider[provider], pkg)
	}

	providers := []string{"npm", "golang", "pypi", "cargo", "github", "gitlab", "codeberg", "gem", "composer", "luarocks", "nuget", "opam", "openvsx", "generic"}
	for _, provider := range providers {
		if packages, exists := packagesByProvider[provider]; exists {
			fmt.Printf("%s %s Packages (%d):\n", IconDiamond(), strings.ToUpper(provider), len(packages))
			for _, pkg := range packages {
				fmt.Printf("   %s %s (v%s)", getProviderIcon(provider), pkg.Source.ID, pkg.Version)
				if pkg.Description != "" {
					fmt.Printf("\n      %s", pkg.Description)
				}
				fmt.Println()
			}
			fmt.Println()
		}
	}
}

// listAllPackagesJSON lists all packages in JSON format
func (ls *ListService) listAllPackagesJSON(filteredRegistry []registry_parser.RegistryItem, opts ListQueryOptions) {
	filters := opts.NameFilters
	result := make(map[string]any)
	result["type"] = "all"
	if len(filters) > 0 {
		result["filters"] = filters
	}
	appendListQueryJSONFields(result, opts)

	if len(filteredRegistry) == 0 {
		result["count"] = 0
		result["packages"] = []any{}
		PrintJSON(result)
		return
	}

	// Get installed packages to check status
	installedPackages := ls.localPackages.GetData(false).Packages
	installedMap := make(map[string]string) // sourceID -> version
	for _, pkg := range installedPackages {
		installedMap[pkg.SourceID] = pkg.Version
	}

	packagesData := make([]map[string]any, 0, len(filteredRegistry))
	for _, pkg := range filteredRegistry {
		packageName := getPackageNameFromSourceID(pkg.Source.ID)
		provider := getProviderFromSourceID(pkg.Source.ID)
		installedVersion, isInstalled := installedMap[pkg.Source.ID]

		pkgData := map[string]any{
			"source_id": pkg.Source.ID,
			"name":      packageName,
			"provider":  provider,
			"version":   pkg.Version,
			"installed": isInstalled,
		}

		if isInstalled {
			pkgData["installed_version"] = installedVersion
			_, hasUpdate := ls.checkUpdateAvailability(pkg.Source.ID, installedVersion, "")
			pkgData["has_update"] = hasUpdate
		}

		if pkg.Description != "" {
			pkgData["description"] = pkg.Description
		}

		packagesData = append(packagesData, pkgData)
	}

	result["count"] = len(filteredRegistry)
	result["packages"] = packagesData
	PrintJSON(result)
}

// checkUpdateAvailability checks if an update is available for a package.
// installedCommit is optional; when set alongside a cached remote commit for
// non-registry git packages, commit inequality decides outdated status.
func (ls *ListService) checkUpdateAvailability(sourceID, currentVersion, installedCommit string) (string, bool) {
	stable, prerelease, remoteCommit := resolveUpdateCandidates(ls.registry, sourceID)
	if stable == "" && prerelease == "" {
		return "", false // No registry or remote-latest info available
	}
	latestVersion := chooseBestRemoteVersion(currentVersion, stable, prerelease)
	if providers.GitCommitStillNeedsUpdate(sourceID, latestVersion, installedCommit, remoteCommit) {
		return fmt.Sprintf("%s Update available: v%s", IconRefresh(), latestVersion), true
	}
	if strings.TrimSpace(installedCommit) != "" && strings.TrimSpace(remoteCommit) != "" {
		// Commits match (or remote_latest says install matches the reconciled tip):
		// treat as up to date even if version strings differ (e.g. branch aliases).
		return IconCheckCircle() + " Up to date", false
	}
	// If local version is unknown or set to "latest", always show update to the concrete remote version
	if currentVersion == "" || currentVersion == "latest" {
		return fmt.Sprintf("%s Update available: v%s", IconRefresh(), latestVersion), true
	}
	updateAvailable, _ := ls.updateChecker.CheckIfUpdateIsAvailable(currentVersion, latestVersion)
	if updateAvailable {
		return fmt.Sprintf("%s Update available: v%s", IconRefresh(), latestVersion), true
	}
	return IconCheckCircle() + " Up to date", false
}

// resolveUpdateCandidates returns registry stable/prerelease versions, falling back to
// the cached remote_latest entry for packages not present in the registry. Git-hosted
// registry packages may still use remote_latest when it is a prefer-branch ref.
func resolveUpdateCandidates(registry RegistryProvider, sourceID string) (stable, prerelease, remoteCommit string) {
	item := getRegistryItem(registry, sourceID)
	// Release-asset packages must track GitHub/GitLab/Codeberg *releases*, not git tags.
	// /releases/latest skips pre-releases (ols `nightly`); some git tags also have no assets.
	if len(item.Source.Asset) > 0 {
		if tag, err := providers.LatestReleaseTagForSource(sourceID); err == nil {
			if v := strings.TrimSpace(tag); v != "" {
				return v, "", ""
			}
		}
		stable, prerelease = registry.GetLatestVersions(sourceID)
		if strings.TrimSpace(stable) == "" {
			stable = item.Version
		}
		return stable, prerelease, ""
	}
	if item.Git != nil && len(item.Git.Refs) > 0 {
		if result, _, ok := providers.ResolveGitLatestFromRegistry(item, providers.PreferBranchPolicyForSourceID(sourceID)); ok {
			return result.Version, "", result.Commit
		}
	}
	stable, prerelease = registry.GetLatestVersions(sourceID)
	if providers.IsGitHostedSourceID(sourceID) {
		if entry, ok, err := providers.GetRemoteLatest(sourceID); err == nil && ok {
			if providers.PreferRemoteLatestOverRegistry(entry, stable, prerelease) {
				return entry.Version, "", entry.Commit
			}
		}
	}
	if strings.TrimSpace(stable) != "" || strings.TrimSpace(prerelease) != "" {
		return stable, prerelease, ""
	}
	return "", "", ""
}

func getRegistryItem(registry RegistryProvider, sourceID string) registry_parser.RegistryItem {
	if dp, ok := registry.(*defaultRegistryProvider); ok {
		return dp.registryParser().GetBySourceId(sourceID)
	}
	for _, item := range registry.GetData(false) {
		if item.Source.ID == sourceID {
			return item
		}
	}
	return registry_parser.RegistryItem{}
}

func (ls *ListService) getRegistryItem(sourceID string) registry_parser.RegistryItem {
	return getRegistryItem(ls.registry, sourceID)
}

// Default implementations for backward compatibility
type defaultLocalPackagesProvider struct{}
type defaultRegistryProvider struct {
	parser *registry_parser.RegistryParser
}
type defaultUpdateChecker struct{}
type defaultFileDownloader struct{}

func (d *defaultRegistryProvider) registryParser() *registry_parser.RegistryParser {
	if d.parser == nil {
		d.parser = registry_parser.NewDefaultRegistryParser()
	}
	return d.parser
}

func (d *defaultLocalPackagesProvider) GetData(force bool) local_packages_parser.LocalPackageRoot {
	return local_packages_parser.GetData(force)
}

func (d *defaultRegistryProvider) GetData(force bool) []registry_parser.RegistryItem {
	return d.registryParser().GetData(force)
}

func (d *defaultRegistryProvider) GetLatestVersion(sourceID string) string {
	return d.registryParser().GetLatestVersion(sourceID)
}

func (d *defaultRegistryProvider) GetLatestVersions(sourceID string) (string, string) {
	return d.registryParser().GetLatestVersions(sourceID)
}

func (d *defaultUpdateChecker) CheckIfUpdateIsAvailable(currentVersion, latestVersion string) (bool, string) {
	return providers.CheckIfUpdateIsAvailable(currentVersion, latestVersion)
}

// indirection for testability
var (
	downloadAndUnzipRegistryFn      = files.DownloadAndUnzipRegistry
	downloadAndUnzipRegistryQuietFn = files.DownloadAndUnzipRegistryQuiet
)

func (d *defaultFileDownloader) DownloadAndUnzipRegistry() (bool, error) {
	return downloadAndUnzipRegistryFn()
}

func (d *defaultFileDownloader) DownloadAndUnzipRegistryQuiet() (bool, error) {
	return downloadAndUnzipRegistryQuietFn()
}

// Legacy functions for backward compatibility
func listInstalledPackages() {
	service := NewListService()
	service.ListInstalledPackages(ListQueryOptions{})
}

func listAllPackages() {
	service := NewListService()
	service.ListAllPackages(ListQueryOptions{})
}

func checkUpdateAvailability(sourceID, currentVersion string) (string, bool) {
	service := NewListService()
	return service.checkUpdateAvailability(sourceID, currentVersion, "")
}

func getProviderFromSourceID(sourceID string) string {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return "unknown"
	}
	if strings.HasPrefix(sourceID, "pkg:") {
		rest := strings.TrimPrefix(sourceID, "pkg:")
		idx := strings.Index(rest, "/")
		if idx <= 0 || idx >= len(rest)-1 {
			return "unknown"
		}
		return strings.ToLower(rest[:idx])
	}
	idx := strings.Index(sourceID, ":")
	if idx <= 0 {
		return "unknown"
	}
	return strings.ToLower(sourceID[:idx])
}

func getPackageNameFromSourceID(sourceID string) string {
	// Support new format: provider:pkg
	if strings.Contains(sourceID, ":") && !strings.HasPrefix(sourceID, "pkg:") {
		parts := strings.SplitN(sourceID, ":", 2)
		if len(parts) == 2 {
			return parts[1]
		}
	}
	// Legacy format: pkg:provider/pkg
	withoutPrefix := strings.TrimPrefix(sourceID, "pkg:")
	parts := strings.SplitN(withoutPrefix, "/", 2)
	if len(parts) >= 2 {
		return parts[1]
	}
	return sourceID
}

func getProviderIcon(provider string) string {
	switch provider {
	case "npm":
		return IconNPM()
	case "golang":
		return IconGolang()
	case "pypi":
		return IconPython()
	case "cargo":
		return IconCargo()
	case "github":
		return IconGitHub()
	case "gitlab":
		return IconGitLab()
	case "codeberg":
		return IconCodeberg()
	case "gem":
		return IconGem()
	case "composer":
		return IconComposer()
	case "luarocks":
		return IconLuaRocks()
	case "nuget":
		return IconNuGet()
	case "opam":
		return IconOpam()
	case "openvsx":
		return IconOpenVSX()
	case "generic":
		return IconGeneric()
	default:
		return IconGeneric()
	}
}

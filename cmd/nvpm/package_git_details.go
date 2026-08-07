package nvpm

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/config"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/providers"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/spinnerutil"
)

// showGitDetailsProgress controls spinners while show/info fetches live git metadata.
var showGitDetailsProgress = true

func shouldShowGitDetailsSpinner() bool {
	return showGitDetailsProgress && !ShouldUseJSONOutput()
}

func gitRepoSpinnerLabel(sourceID string) string {
	if idx := strings.Index(sourceID, ":"); idx >= 0 && idx < len(sourceID)-1 {
		return sourceID[idx+1:]
	}
	return sourceID
}

func discoverGitRefSnapshotForShow(sourceID, stableTag, prereleaseTag string) ([]providers.GitRefSnapshot, error) {
	fetch := func() ([]providers.GitRefSnapshot, error) {
		return providers.DiscoverGitRefSnapshot(sourceID, stableTag, prereleaseTag)
	}
	if !shouldShowGitDetailsSpinner() {
		return fetch()
	}
	var (
		snaps []providers.GitRefSnapshot
		err   error
	)
	title := fmt.Sprintf("Fetching remote git refs for %s ...", gitRepoSpinnerLabel(sourceID))
	if runErr := spinnerutil.Run(title, func() {
		snaps, err = fetch()
	}); runErr != nil {
		return nil, runErr
	}
	return snaps, err
}

func resolveGitLatestResultForShow(sourceID string) (providers.GitRemoteLatestResult, error) {
	if !shouldShowGitDetailsSpinner() {
		return providers.ResolveGitLatestResult(sourceID)
	}
	var (
		result providers.GitRemoteLatestResult
		err    error
	)
	title := fmt.Sprintf("Resolving latest ref for %s ...", gitRepoSpinnerLabel(sourceID))
	if runErr := spinnerutil.Run(title, func() {
		result, err = providers.ResolveGitLatestResult(sourceID)
	}); runErr != nil {
		return providers.GitRemoteLatestResult{}, runErr
	}
	return result, err
}

type packageGitDetails struct {
	Refs             []registryGitRefDisplay
	UpdateResolution string
	DiscoveryRemote  string
	DiscoveryLocal   string
	Alerts           []string
}

type registryGitRefDisplay struct {
	Ref            string
	Commit         string
	Age            string
	Kind           string
	commitDateUnix int64
}

func collectPackageGitDetails(item registry_parser.RegistryItem, sourceID string) packageGitDetails {
	if !providers.IsGitHostedSourceID(sourceID) {
		return packageGitDetails{}
	}
	out := packageGitDetails{}
	policy := providers.PreferBranchPolicyForSourceID(sourceID)
	out.UpdateResolution = describeUpdateResolution(policy, sourceID)

	var gitRefs []registry_parser.RegistryItemGitRef
	if item.Git != nil && len(item.Git.Refs) > 0 {
		gitRefs = item.Git.Refs
		for _, ow := range item.Git.TagOverwrites {
			out.Alerts = append(out.Alerts, fmt.Sprintf(
				"Tag %s was force-moved on the remote (was %s, now %s)",
				ow.Tag, shortGitSHA(ow.PreviousCommit), shortGitSHA(ow.CurrentCommit),
			))
		}
	} else if snaps, err := discoverGitRefSnapshotForShow(sourceID, item.Version, item.PrereleaseVersion); err == nil && len(snaps) > 0 {
		gitRefs = providers.GitRefsFromSnapshots(snaps)
	} else if entry, ok, err := providers.GetRemoteLatest(sourceID); err == nil && ok && strings.TrimSpace(entry.Version) != "" {
		gitRefs = []registry_parser.RegistryItemGitRef{{
			Ref: entry.Version, Kind: "remote", Commit: entry.Commit,
		}}
	}
	if len(gitRefs) > 0 {
		out.Refs = gitRefDisplaysFromRegistry(gitRefs)
	}

	resolveItem := item
	if len(gitRefs) > 0 {
		resolveItem.Git = &registry_parser.RegistryItemGit{Refs: gitRefs}
	}
	result, decision, ok := providers.ResolveGitLatestFromRegistry(resolveItem, policy)
	if !ok && item.Source.ID != "" {
		if r, err := resolveGitLatestResultForShow(sourceID); err == nil {
			result = r
			ok = true
		}
	}
	if ok {
		if decision.Reason == "release-age-gap" && result.SupersededTag != "" {
			out.UpdateResolution += fmt.Sprintf(" (chose branch %s over stale tag %s)", result.Version, result.SupersededTag)
		}
		out.DiscoveryRemote = describeRemoteCommit(gitRefs, result.Version, result.Commit)
		if t, seen, err := providers.GetFirstSeen(sourceID, providers.FormatGitDiscoveryVersionForRef(result.Version, result.Commit)); err == nil && seen {
			out.DiscoveryLocal = fmt.Sprintf("first recorded locally %s", formatAgeAgo(time.Since(t)))
		}
	}

	lockItem := local_packages_parser.GetBySourceId(sourceID)
	if lockItem.Version != "" && lockItem.Commit != "" {
		installedKey := providers.FormatGitDiscoveryVersionForRef(lockItem.Version, lockItem.Commit)
		if t, seen, err := providers.GetFirstSeen(sourceID, installedKey); err == nil && seen {
			installedLocal := fmt.Sprintf("installed %s (%s) first recorded locally %s",
				lockItem.Version, shortGitSHA(lockItem.Commit), formatAgeAgo(time.Since(t)))
			if out.DiscoveryLocal == "" {
				out.DiscoveryLocal = installedLocal
			} else if !strings.Contains(out.DiscoveryLocal, lockItem.Version) {
				out.DiscoveryLocal += "; " + installedLocal
			}
		}
	}
	out.Alerts = append(out.Alerts, localTagOverwriteAlerts(sourceID, lockItem, gitRefs)...)

	return out
}

func gitRefDisplaysFromRegistry(refs []registry_parser.RegistryItemGitRef) []registryGitRefDisplay {
	now := time.Now()
	out := make([]registryGitRefDisplay, 0, len(refs))
	for _, r := range refs {
		age := "-"
		if r.CommitDateUnix > 0 {
			age = formatAgeAgo(now.Sub(time.Unix(r.CommitDateUnix, 0)))
		}
		out = append(out, registryGitRefDisplay{
			Ref: r.Ref, Commit: shortGitSHA(r.Commit), Age: age, Kind: r.Kind,
			commitDateUnix: r.CommitDateUnix,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			if out[i].Kind == "branch" {
				return true
			}
			if out[j].Kind == "branch" {
				return false
			}
		}
		if out[i].commitDateUnix != out[j].commitDateUnix {
			return out[i].commitDateUnix > out[j].commitDateUnix
		}
		return out[i].Ref < out[j].Ref
	})
	return out
}

func describeUpdateResolution(policy providers.PreferBranchPolicy, sourceID string) string {
	lockItem := local_packages_parser.GetBySourceId(sourceID)
	source := "global config defaults"
	if lockItem.Extras != nil && lockItem.Extras.UpdateResolution != nil {
		source = "lock file override"
	}
	branches := strings.Join(policy.Branches, ", ")
	switch policy.Kind {
	case config.PreferBranchWhenAlways:
		return fmt.Sprintf("%s: always prefer branch tips (%s)", source, branches)
	default:
		return fmt.Sprintf("%s: prefer branch (%s) when latest tag/release is ≥ %s old and branch is newer",
			source, branches, formatInDuration(policy.Gap))
	}
}

func describeRemoteCommit(refs []registry_parser.RegistryItemGitRef, ref, commit string) string {
	for _, r := range refs {
		if strings.EqualFold(r.Ref, ref) && (commit == "" || strings.EqualFold(r.Commit, commit)) {
			if r.CommitDateUnix > 0 {
				return fmt.Sprintf("%s (%s) committed %s on remote",
					ref, shortGitSHA(r.Commit), formatAgeAgo(time.Since(time.Unix(r.CommitDateUnix, 0))))
			}
			if r.Commit != "" {
				return fmt.Sprintf("%s (%s)", ref, shortGitSHA(r.Commit))
			}
			break
		}
	}
	if commit != "" {
		return fmt.Sprintf("%s (%s)", ref, shortGitSHA(commit))
	}
	return ref
}

func localTagOverwriteAlerts(sourceID string, lockItem local_packages_parser.LocalPackageItem, refs []registry_parser.RegistryItemGitRef) []string {
	if lockItem.Version == "" || lockItem.Commit == "" {
		return nil
	}
	var alerts []string
	key := providers.FormatGitDiscoveryVersionForRef(lockItem.Version, lockItem.Commit)
	if _, seen, err := providers.GetFirstSeen(sourceID, key); err != nil || !seen {
		return alerts
	}
	for _, r := range refs {
		if r.Kind != "tag" || !strings.EqualFold(r.Ref, lockItem.Version) {
			continue
		}
		if !strings.EqualFold(r.Commit, lockItem.Commit) {
			alerts = append(alerts, fmt.Sprintf(
				"Installed tag/release %s (%s) no longer matches remote (%s)",
				lockItem.Version, shortGitSHA(lockItem.Commit), shortGitSHA(r.Commit),
			))
		}
	}
	return alerts
}

func appendGitDetailsMarkdown(b *strings.Builder, details packageGitDetails) {
	if len(details.Refs) == 0 && details.UpdateResolution == "" && details.DiscoveryRemote == "" && len(details.Alerts) == 0 {
		return
	}
	if len(details.Refs) > 0 {
		b.WriteString("## Remote refs\n\n")
		for _, r := range details.Refs {
			b.WriteString(fmt.Sprintf("- %s (%s) - %s\n", r.Ref, r.Commit, r.Age))
		}
		b.WriteString("\n")
	}
	if details.UpdateResolution != "" {
		b.WriteString("## Update resolution\n\n")
		b.WriteString(details.UpdateResolution + "\n\n")
	}
	if details.DiscoveryRemote != "" || details.DiscoveryLocal != "" {
		b.WriteString("## Discovery\n\n")
		if details.DiscoveryRemote != "" {
			b.WriteString(fmt.Sprintf("- Remote: %s\n", details.DiscoveryRemote))
		}
		if details.DiscoveryLocal != "" {
			b.WriteString(fmt.Sprintf("- Local: %s\n", details.DiscoveryLocal))
		}
		b.WriteString("\n")
	}
	if len(details.Alerts) > 0 {
		b.WriteString("## Alerts\n\n")
		for _, a := range details.Alerts {
			b.WriteString(fmt.Sprintf("- ⚠️ %s\n", a))
		}
		b.WriteString("\n")
	}
}

func appendGitDetailsPlain(b *strings.Builder, details packageGitDetails) {
	if len(details.Refs) == 0 && details.UpdateResolution == "" && details.DiscoveryRemote == "" && len(details.Alerts) == 0 {
		return
	}
	if len(details.Refs) > 0 {
		b.WriteString("Remote refs:\n")
		for _, r := range details.Refs {
			b.WriteString(fmt.Sprintf("  - %s (%s) - %s\n", r.Ref, r.Commit, r.Age))
		}
	}
	if details.UpdateResolution != "" {
		b.WriteString(fmt.Sprintf("Update resolution: %s\n", details.UpdateResolution))
	}
	if details.DiscoveryRemote != "" {
		b.WriteString(fmt.Sprintf("Remote latest: %s\n", details.DiscoveryRemote))
	}
	if details.DiscoveryLocal != "" {
		b.WriteString(fmt.Sprintf("Local discovery: %s\n", details.DiscoveryLocal))
	}
	for _, a := range details.Alerts {
		b.WriteString(fmt.Sprintf("Alert: %s\n", a))
	}
}

func mergeGitDetailsJSON(result map[string]any, details packageGitDetails) {
	if len(details.Refs) == 0 && details.UpdateResolution == "" && len(details.Alerts) == 0 {
		return
	}
	if len(details.Refs) > 0 {
		refs := make([]map[string]string, 0, len(details.Refs))
		for _, r := range details.Refs {
			refs = append(refs, map[string]string{
				"ref": r.Ref, "commit": r.Commit, "age": r.Age, "kind": r.Kind,
			})
		}
		result["git_refs"] = refs
	}
	if details.UpdateResolution != "" {
		result["update_resolution"] = details.UpdateResolution
	}
	if details.DiscoveryRemote != "" || details.DiscoveryLocal != "" {
		result["discovery"] = map[string]string{
			"remote": details.DiscoveryRemote,
			"local":  details.DiscoveryLocal,
		}
	}
	if len(details.Alerts) > 0 {
		result["alerts"] = details.Alerts
	}
}

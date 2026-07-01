package providers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mattn/go-isatty"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/shell_out"
	"github.com/mistweaverco/nvpm-client/internal/lib/treesitterdeps"
	"golang.org/x/mod/semver"
)

// External tree-sitter query repositories (e.g. neovim-treesitter/nvim-treesitter-queries-*)
// are optional community sources. Policy controls prompts and non-interactive behavior.

type externalTreeSitterQueriesPolicyKind int

const (
	externalTreeSitterQueriesAsk externalTreeSitterQueriesPolicyKind = iota
	externalTreeSitterQueriesAlways
	externalTreeSitterQueriesNever
)

var externalTreeSitterQueriesPolicyValue = externalTreeSitterQueriesAsk

var (
	externalQueriesShellOutCapture = shell_out.ShellOutCapture
	externalQueriesGitHas          = shell_out.HasCommand
)

// externalQueriesGitRevParse reads HEAD after a successful clone (tests may stub).
var externalQueriesGitRevParse = defaultExternalQueriesGitRevParse

func defaultExternalQueriesGitRevParse(dir string) (string, error) {
	code, out, err := externalQueriesShellOutCapture("git", []string{"-C", dir, "rev-parse", "HEAD"}, "", nil)
	if err != nil || code != 0 {
		return "", fmt.Errorf("git rev-parse HEAD in %q: %w %s", dir, err, strings.TrimSpace(out))
	}
	return strings.TrimSpace(out), nil
}

func normalizeExternalQueryRepoURL(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimSuffix(u, "/")
	u = strings.TrimSuffix(strings.TrimSuffix(u, ".git"), "/")
	return strings.ToLower(u)
}

func externalQueryRepoURLsEqual(a, b string) bool {
	return normalizeExternalQueryRepoURL(a) == normalizeExternalQueryRepoURL(b)
}

// externalQueryLockPinFromLocalLock returns a pinned commit from nvpm-lock.json when the lock
// row matches this grammar package version and optional external query repo URL (reproducible sync).
func externalQueryLockPinFromLocalLock(sourceID, version, lang, wantRepoURL string) (repoURL, ref string, ok bool) {
	item := local_packages_parser.GetBySourceId(sourceID)
	if item.SourceID == "" || strings.TrimSpace(item.Version) != strings.TrimSpace(version) {
		return "", "", false
	}
	if item.Extras == nil {
		return "", "", false
	}
	wantLang := strings.ToLower(strings.TrimSpace(lang))
	wantURL := strings.TrimSpace(wantRepoURL)
	for _, p := range item.Extras.TreeSitterExternalQueries {
		if strings.ToLower(strings.TrimSpace(p.Language)) != wantLang {
			continue
		}
		cand := strings.TrimSpace(p.RepoURL)
		if wantURL != "" && !externalQueryRepoURLsEqual(cand, wantURL) {
			continue
		}
		ref = strings.TrimSpace(p.Ref)
		repoURL = cand
		if ref != "" && repoURL != "" {
			return repoURL, ref, true
		}
		return "", "", false
	}
	return "", "", false
}

// externalQueryLockPinForConfirmFilter is the lock lookup used when deciding whether to prompt for
// external query clones; tests may replace it.
var externalQueryLockPinForConfirmFilter = externalQueryLockPinFromLocalLock

func externalQueryLockCoversNeed(sourceID, version string, n externalQueryNeed) bool {
	lockRepo, lockRef, ok := externalQueryLockPinForConfirmFilter(sourceID, version, n.Lang, n.URL)
	if !ok || strings.TrimSpace(lockRef) == "" {
		return false
	}
	return externalQueryRepoURLsEqual(lockRepo, n.URL)
}

func externalQueryNeedKey(lang, repoURL string) string {
	return strings.ToLower(strings.TrimSpace(lang)) + "\x00" + normalizeExternalQueryRepoURL(repoURL)
}

// externalQueryNeedsStillRequiringConfirm returns only those external query needs for which the
// lockfile does not already record an acceptable pin (same grammar version, repo URL, non-empty ref).
// Sync and reinstall can then skip re-prompting for those languages.
func externalQueryNeedsStillRequiringConfirm(sourceID, version string, needs []externalQueryNeed) []externalQueryNeed {
	if len(needs) == 0 {
		return nil
	}
	var out []externalQueryNeed
	for _, n := range needs {
		if externalQueryLockCoversNeed(sourceID, version, n) {
			continue
		}
		out = append(out, n)
	}
	return out
}

// cloneExternalQueriesRepo clones repoURL into destDir, optionally checking out a tag/branch/commit.
// When lockRef is set and lockRepoURL matches repoURL, lockRef is used instead of registry semver/ref.
// Returns the resolved full commit SHA of HEAD.
func cloneExternalQueriesRepo(repoURL, destDir, registryRef string, wantSemver bool, lockRepoURL, lockRef string) (string, error) {
	if !externalQueriesGitHas("git", []string{"--version"}, nil) {
		return "", fmt.Errorf("git not found in PATH (required to clone external tree-sitter query repositories)")
	}
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return "", fmt.Errorf("external queries repo_url is empty")
	}
	if err := os.RemoveAll(destDir); err != nil {
		return "", fmt.Errorf("remove old external queries clone: %w", err)
	}
	parent := filepath.Dir(destDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("mkdir external queries parent: %w", err)
	}

	useLock := strings.TrimSpace(lockRef) != "" && externalQueryRepoURLsEqual(lockRepoURL, repoURL)

	var checkout string
	if useLock {
		checkout = strings.TrimSpace(lockRef)
	} else {
		checkout = strings.TrimSpace(registryRef)
		if checkout == "" && wantSemver {
			code, out, err := externalQueriesShellOutCapture("git", []string{"ls-remote", "--tags", repoURL}, "", nil)
			if err != nil || code != 0 {
				return "", fmt.Errorf("git ls-remote --tags %q: %w %s", repoURL, err, strings.TrimSpace(out))
			}
			var semverOK bool
			checkout, semverOK = pickLatestSemverTag(out)
			if !semverOK {
				checkout = ""
			}
		}
	}

	if err := gitCloneExternalQueryRepo(repoURL, destDir, checkout, parent); err != nil {
		return "", err
	}
	return externalQueriesGitRevParse(destDir)
}

func gitCloneExternalQueryRepo(repoURL, destDir, checkout, parent string) error {
	args := []string{"clone", "--depth", "1"}
	if checkout != "" {
		args = append(args, "-b", checkout)
	}
	args = append(args, repoURL, destDir)
	code, out, err := externalQueriesShellOutCapture("git", args, parent, nil)
	if err == nil && code == 0 {
		return nil
	}
	_ = os.RemoveAll(destDir)
	code2, out2, err2 := externalQueriesShellOutCapture("git", []string{"clone", repoURL, destDir}, parent, nil)
	if err2 != nil || code2 != 0 {
		return fmt.Errorf("git clone %q: %w %s (shallow failed: %v %s)", repoURL, err2, strings.TrimSpace(out2), err, strings.TrimSpace(out))
	}
	if strings.TrimSpace(checkout) == "" {
		return nil
	}
	code3, out3, err3 := externalQueriesShellOutCapture("git", []string{"-C", destDir, "checkout", "--detach", checkout}, "", nil)
	if err3 != nil || code3 != 0 {
		return fmt.Errorf("git checkout %q in %q: %w %s", checkout, destDir, err3, strings.TrimSpace(out3))
	}
	return nil
}

// externalTreeSitterQueriesConfirmHook is swapped in tests.
var externalTreeSitterQueriesConfirmHook = defaultExternalTreeSitterQueriesConfirm

func parseExternalTreeSitterQueriesPolicy(s string) (externalTreeSitterQueriesPolicyKind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "ask":
		return externalTreeSitterQueriesAsk, nil
	case "always", "yes", "true", "1":
		return externalTreeSitterQueriesAlways, nil
	case "never", "no", "false", "0":
		return externalTreeSitterQueriesNever, nil
	default:
		return externalTreeSitterQueriesAsk, fmt.Errorf("invalid policy %q (use ask, always, or never)", s)
	}
}

// SetExternalTreeSitterQueriesPolicy configures how optional external query git clones are handled.
func SetExternalTreeSitterQueriesPolicy(s string) error {
	p, err := parseExternalTreeSitterQueriesPolicy(s)
	if err != nil {
		return err
	}
	externalTreeSitterQueriesPolicyValue = p
	return nil
}

// ConfigureExternalTreeSitterQueriesFromCLI applies the CLI flag when set; otherwise reads
// NVPM_EXTERNAL_TREESITTER_QUERIES (ask | always | never).
func ConfigureExternalTreeSitterQueriesFromCLI(flagExplicit bool, flagValue string) error {
	val := strings.TrimSpace(flagValue)
	if !flagExplicit {
		if e := strings.TrimSpace(os.Getenv("NVPM_EXTERNAL_TREESITTER_QUERIES")); e != "" {
			val = e
		}
	}
	return SetExternalTreeSitterQueriesPolicy(val)
}

type externalQueryNeed struct {
	Lang string
	URL  string
}

// ExternalQueryPreflightChoice records phased-install preflight consent for optional external query
// git clones. When non-nil, the cache step skips batch confirmation. AllowUnpinned applies only to
// language/repo pairs not already covered by a matching lockfile pin (same grammar version, repo URL, ref).
type ExternalQueryPreflightChoice struct {
	AllowUnpinned bool
}

func resolvedExternalQueryRepoURL(spec registry_parser.RegistryItemTreeSitterExternalQueries) (string, error) {
	reg := registry_parser.NewDefaultRegistryParser()
	return treesitterdeps.ResolveExternalQueryRepoURL(spec, reg)
}

// plannedTreeSitterBuildLanguages returns every non-empty language from the registry build list
// (used before parsers are built, e.g. external-query preflight).
func plannedTreeSitterBuildLanguages(build []registry_parser.RegistryItemTreeSitterBuild) []string {
	var out []string
	for _, b := range build {
		if s := strings.TrimSpace(b.Language); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func collectExternalTreeSitterQueryNeeds(
	repoPath string,
	build []registry_parser.RegistryItemTreeSitterBuild,
	builtLangs []string,
) ([]externalQueryNeed, error) {
	want := map[string]struct{}{}
	for _, l := range builtLangs {
		l = strings.TrimSpace(l)
		if l != "" {
			want[l] = struct{}{}
		}
	}
	var out []externalQueryNeed
	seen := map[string]struct{}{}
	for _, b := range build {
		lang := strings.TrimSpace(b.Language)
		grammarDir := strings.TrimSpace(b.GrammarDir)
		if lang == "" {
			continue
		}
		if !b.QueriesOnly && grammarDir == "" {
			continue
		}
		if !TreeSitterBuildDeclaresNeovimIntegration(b) {
			continue
		}
		if _, ok := want[lang]; !ok {
			continue
		}
		if len(b.ExternalQueries) == 0 {
			continue
		}
		for _, spec := range b.ExternalQueries {
			url, err := resolvedExternalQueryRepoURL(spec)
			if err != nil {
				return nil, err
			}
			url = strings.TrimSpace(url)
			if url == "" {
				continue
			}
			// Registry external_queries always participates in confirm / policy. Upstream repos often
			// ship a minimal queries/ tree; that must not hide the optional nvim-treesitter-queries-* clone.
			k := externalQueryNeedKey(lang, url)
			if _, dup := seen[k]; dup {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, externalQueryNeed{Lang: lang, URL: url})
		}
	}
	return out, nil
}

func batchConfirmExternalTreeSitterQueries(sourceID string, needs []externalQueryNeed) (bool, error) {
	if len(needs) == 0 {
		return true, nil
	}
	switch externalTreeSitterQueriesPolicyValue {
	case externalTreeSitterQueriesNever:
		return false, nil
	case externalTreeSitterQueriesAlways:
		return true, nil
	default:
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Package %s can fetch Neovim tree-sitter queries from these separate repositories (not the parser upstream):\n\n", sourceID)
	for _, n := range needs {
		fmt.Fprintf(&b, "• language %q → %s\n", n.Lang, n.URL)
	}
	b.WriteString("\nAllow NVPM to git clone these repositories on your machine?")

	title := "External tree-sitter query sources"
	return externalTreeSitterQueriesConfirmHook(title, b.String())
}

func defaultExternalTreeSitterQueriesConfirm(title, description string) (bool, error) {
	if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stderr.Fd()) {
		return false, fmt.Errorf(
			"%s\n%s\n\nNon-interactive session: set NVPM_EXTERNAL_TREESITTER_QUERIES=always to allow these clones, or never to skip without prompting",
			title,
			description,
		)
	}
	// Default to Download (affirmative) so Enter selects the primary action.
	proceed := true
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(title).
				Description(description).
				Value(&proceed).
				Affirmative("Download").
				Negative("Skip (no external queries)"),
		),
	)
	if err := form.Run(); err != nil {
		return false, err
	}
	return proceed, nil
}

func canonicalSemverTag(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ""
	}
	if semver.IsValid(tag) {
		return semver.Canonical(tag)
	}
	v := "v" + strings.TrimPrefix(strings.TrimPrefix(tag, "V"), "v")
	if semver.IsValid(v) {
		return semver.Canonical(v)
	}
	return ""
}

func pickLatestSemverTag(lsRemoteOutput string) (tag string, ok bool) {
	var bestTag string
	var bestCanon string
	for _, line := range strings.Split(lsRemoteOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		ref := parts[1]
		const prefix = "refs/tags/"
		if !strings.HasPrefix(ref, prefix) {
			continue
		}
		t := strings.TrimPrefix(ref, prefix)
		if strings.HasSuffix(t, "^{}") {
			continue
		}
		canon := canonicalSemverTag(t)
		if canon == "" {
			continue
		}
		if bestCanon == "" || semver.Compare(canon, bestCanon) > 0 {
			bestCanon = canon
			bestTag = t
		}
	}
	if bestTag == "" {
		return "", false
	}
	return bestTag, true
}

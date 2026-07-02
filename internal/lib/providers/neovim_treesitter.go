package providers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mattn/go-isatty"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/shell_out"
	"github.com/mistweaverco/nvpm-client/internal/lib/spinnerutil"
	"github.com/mistweaverco/nvpm-client/internal/lib/treesitterdeps"
)

// Injectable helpers for tests
var (
	neovimShellOutCapture = shell_out.ShellOutCapture
	neovimMkdirAll        = os.MkdirAll
	neovimRemove          = os.Remove
	neovimRemoveAll       = os.RemoveAll
	neovimStat            = os.Stat
	neovimUserHomeDir     = os.UserHomeDir
	neovimGetenv          = os.Getenv
	neovimReadFile        = os.ReadFile
	neovimWriteFile       = os.WriteFile
)

// neovimInheritsPromptAction is returned by [neovimInheritsPrompt] (injectable for tests).
type neovimInheritsPromptAction string

const (
	neovimInheritsAbort    neovimInheritsPromptAction = "abort"
	neovimInheritsContinue neovimInheritsPromptAction = "continue"
	neovimInheritsInstall  neovimInheritsPromptAction = "install"
)

// neovimInheritsPrompt is swapped in tests to avoid interactive huh.
var neovimInheritsPrompt = defaultNeovimInheritsPrompt

func defaultNeovimInheritsPrompt(title, description string) (neovimInheritsPromptAction, error) {
	if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stderr.Fd()) {
		return neovimInheritsAbort, fmt.Errorf("%s\n%s", title, description)
	}
	var choice neovimInheritsPromptAction = neovimInheritsInstall
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[neovimInheritsPromptAction]().
				Title(title).
				Description(description).
				Options(
					huh.NewOption("Install missing dependencies", neovimInheritsInstall),
					huh.NewOption("Continue anyway", neovimInheritsContinue),
					huh.NewOption("Abort", neovimInheritsAbort),
				).
				Value(&choice),
		),
	)
	if err := form.Run(); err != nil {
		return neovimInheritsAbort, err
	}
	return choice, nil
}

// NeovimInheritInstallNotifierStart, if non-nil, is called before each inherited tree-sitter
// grammar install (e.g. CLI prints progress while the parent install spinner is running).
var NeovimInheritInstallNotifierStart func(sourceID string, registryVersion string)

// NeovimInheritInstallNotifierDone, if non-nil, is called after each successful inherited install
// with the version from the lockfile (matches integration report keys).
var NeovimInheritInstallNotifierDone func(sourceID string, resolvedVersion string)

func neovimTreeSitterQueriesCacheDir(sourceID, version, lang string) string {
	return filepath.Join(TreeSitterArtifactVersionDir(sourceID, version), "queries", lang)
}

func externalQueryCloneWorkDir(sourceID, version, lang, repoURL string) string {
	sum := sha256.Sum256([]byte(normalizeExternalQueryRepoURL(repoURL)))
	return filepath.Join(TreeSitterArtifactVersionDir(sourceID, version), "external-query-clones", lang, hex.EncodeToString(sum[:8]))
}

// resolveNeovimTreeSitterQueriesDir finds highlights/injections (etc.) for Neovim under a grammar checkout.
// Prefers grammar-local queries, then repo-root queries (for grammars in subdirectories).
func resolveNeovimTreeSitterQueriesDir(repoPath, fullGrammarDir string) string {
	candidates := []string{
		filepath.Join(fullGrammarDir, "queries"),
		filepath.Join(repoPath, "queries"),
	}
	seen := map[string]struct{}{}
	for _, c := range candidates {
		key := filepath.Clean(c)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
	}
	return ""
}

func hasTreeSitterInheritsModeline(content []byte) bool {
	nonEmpty := 0
	for _, line := range bytes.Split(content, []byte{'\n'}) {
		t := bytes.TrimSpace(line)
		if len(t) == 0 {
			continue
		}
		nonEmpty++
		if nonEmpty > 8 {
			break
		}
		if !bytes.HasPrefix(t, []byte(";")) {
			return false
		}
		low := bytes.ToLower(t)
		if bytes.Contains(low, []byte("inherits:")) {
			return true
		}
	}
	return false
}

func patchNeovimTreeSitterSCM(content []byte, inherits []string) []byte {
	s := strings.ReplaceAll(string(content), "#is-not?", "#not-eq?")
	b := []byte(s)
	if len(inherits) == 0 {
		return b
	}
	clean := make([]string, 0, len(inherits))
	seen := map[string]struct{}{}
	for _, in := range inherits {
		in = strings.TrimSpace(in)
		if in == "" {
			continue
		}
		k := strings.ToLower(in)
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		clean = append(clean, in)
	}
	if len(clean) == 0 || hasTreeSitterInheritsModeline(b) {
		return b
	}
	prefix := "; inherits: " + strings.Join(clean, ", ") + "\n"
	return append([]byte(prefix), b...)
}

func copyNeovimTreeSitterQueriesDir(src, dst string, inherits []string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.EqualFold(filepath.Ext(path), ".scm") {
			b = patchNeovimTreeSitterSCM(b, inherits)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}

func cacheNeovimTreeSitterQueriesAfterBuild(
	repoPath, fullGrammarDir, sourceID, version string,
	build registry_parser.RegistryItemTreeSitterBuild,
	allowExternalQueryClone func(lang, repoURL string) bool,
) ([]local_packages_parser.TreeSitterExternalQueryPin, error) {
	lang := strings.TrimSpace(build.Language)
	if lang == "" {
		return nil, nil
	}
	if !TreeSitterBuildDeclaresNeovimIntegration(build) || !integrationEnabled("neovim") {
		return nil, nil
	}
	dest := neovimTreeSitterQueriesCacheDir(sourceID, version, lang)
	if err := os.RemoveAll(dest); err != nil {
		return nil, fmt.Errorf("clear cached queries for %s: %w", lang, err)
	}

	var pins []local_packages_parser.TreeSitterExternalQueryPin
	for _, spec := range build.ExternalQueries {
		repoURL, err := resolvedExternalQueryRepoURL(spec)
		if err != nil {
			return nil, err
		}
		repoURL = strings.TrimSpace(repoURL)
		if repoURL == "" {
			continue
		}
		if !allowExternalQueryClone(lang, repoURL) {
			continue
		}
		lockRepo, lockRef, haveLock := externalQueryLockPinFromLocalLock(sourceID, version, lang, repoURL)
		if haveLock && !externalQueryRepoURLsEqual(lockRepo, repoURL) {
			haveLock = false
		}
		var lockR, lockRefVal string
		if haveLock {
			lockR, lockRefVal = lockRepo, lockRef
		}
		cloneDir := externalQueryCloneWorkDir(sourceID, version, lang, repoURL)
		cloneTitle := fmt.Sprintf("Cloning external tree-sitter queries (%s)...", lang)
		var resolved string
		var cloneErr error
		if spinErr := spinnerutil.RunIfTTY(cloneTitle, func() {
			resolved, cloneErr = cloneExternalQueriesRepo(
				repoURL,
				cloneDir,
				spec.Ref,
				spec.Semver,
				lockR,
				lockRefVal,
			)
		}); spinErr != nil {
			return nil, fmt.Errorf("external queries for %s: %w", lang, spinErr)
		}
		if cloneErr != nil {
			return nil, fmt.Errorf("external queries for %s: %w", lang, cloneErr)
		}
		extSrc := resolveNeovimTreeSitterQueriesDir(cloneDir, cloneDir)
		if extSrc == "" {
			return nil, fmt.Errorf("external queries repo %s has no queries/ directory usable for language %s", repoURL, lang)
		}
		if err := copyNeovimTreeSitterQueriesDir(extSrc, dest, build.Inherits); err != nil {
			return nil, fmt.Errorf("cache external tree-sitter queries for %s: %w", lang, err)
		}
		pins = append(pins, local_packages_parser.TreeSitterExternalQueryPin{
			Language: lang,
			RepoURL:  repoURL,
			Ref:      resolved,
		})
	}
	if len(pins) > 0 {
		return pins, nil
	}

	if src := resolveNeovimTreeSitterQueriesDir(repoPath, fullGrammarDir); src != "" {
		if err := copyNeovimTreeSitterQueriesDir(src, dest, build.Inherits); err != nil {
			return nil, fmt.Errorf("cache tree-sitter queries for %s: %w", lang, err)
		}
		return nil, nil
	}
	return nil, nil
}

func cacheNeovimTreeSitterQueriesForBuiltLangs(
	repoPath, sourceID, version string,
	build []registry_parser.RegistryItemTreeSitterBuild,
	builtLangs []string,
	externalPreflight *ExternalQueryPreflightChoice,
) ([]local_packages_parser.TreeSitterExternalQueryPin, error) {
	var pins []local_packages_parser.TreeSitterExternalQueryPin
	want := map[string]struct{}{}
	for _, l := range builtLangs {
		l = strings.TrimSpace(l)
		if l != "" {
			want[l] = struct{}{}
		}
	}
	var allowExternalQueryClone func(lang, repoURL string) bool
	if !integrationEnabled("neovim") {
		allowExternalQueryClone = func(string, string) bool { return false }
	} else if externalPreflight != nil {
		needs, err := collectExternalTreeSitterQueryNeeds(repoPath, build, builtLangs)
		if err != nil {
			return nil, err
		}
		allowUnpinned := externalPreflight.AllowUnpinned
		pinned := make(map[string]struct{}, len(needs))
		for _, n := range needs {
			if externalQueryLockCoversNeed(sourceID, version, n) {
				pinned[externalQueryNeedKey(n.Lang, n.URL)] = struct{}{}
			}
		}
		allowExternalQueryClone = func(lang, repoURL string) bool {
			if _, ok := pinned[externalQueryNeedKey(lang, repoURL)]; ok {
				return true
			}
			return allowUnpinned
		}
	} else {
		needs, err := collectExternalTreeSitterQueryNeeds(repoPath, build, builtLangs)
		if err != nil {
			return nil, err
		}
		needsConfirm := externalQueryNeedsStillRequiringConfirm(sourceID, version, needs)
		allowUnpinned := true
		if len(needsConfirm) > 0 {
			var confirmErr error
			allowUnpinned, confirmErr = batchConfirmExternalTreeSitterQueries(sourceID, needsConfirm)
			if confirmErr != nil {
				return nil, confirmErr
			}
		}
		pinned := make(map[string]struct{}, len(needs))
		for _, n := range needs {
			if externalQueryLockCoversNeed(sourceID, version, n) {
				pinned[externalQueryNeedKey(n.Lang, n.URL)] = struct{}{}
			}
		}
		allowExternalQueryClone = func(lang, repoURL string) bool {
			if _, ok := pinned[externalQueryNeedKey(lang, repoURL)]; ok {
				return true
			}
			return allowUnpinned
		}
	}
	for _, b := range build {
		lang := strings.TrimSpace(b.Language)
		grammarDir := strings.TrimSpace(b.GrammarDir)
		if lang == "" {
			continue
		}
		if !b.QueriesOnly && grammarDir == "" {
			continue
		}
		if _, ok := want[lang]; !ok {
			continue
		}
		fullGrammarDir := repoPath
		if grammarDir != "" {
			fullGrammarDir = filepath.Join(repoPath, filepath.FromSlash(grammarDir))
		}
		newPins, err := cacheNeovimTreeSitterQueriesAfterBuild(repoPath, fullGrammarDir, sourceID, version, b, allowExternalQueryClone)
		if err != nil {
			return nil, err
		}
		if len(newPins) > 0 {
			pins = append(pins, newPins...)
		}
	}
	return pins, nil
}

// neovimQueryLanguageNameRe restricts language names embedded into a Neovim -c lua snippet.
var neovimQueryLanguageNameRe = regexp.MustCompile(`(?i)^[a-z][a-z0-9_-]*$`)

// neovimBundledQueriesPresent reports whether Neovim's own runtime ships highlights.scm for lang
// (nvim --clean, no user site). When true, installing grammar-local copies into ~/.local/share/nvim/site/queries
// shadows the bundled queries and can break vim.treesitter.start (e.g. markdown + LSP preview buffers).
func neovimBundledQueriesPresent(lang string) (bool, error) {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return false, nil
	}
	if !neovimQueryLanguageNameRe.MatchString(lang) {
		return false, fmt.Errorf("invalid tree-sitter language name for Neovim query check: %q", lang)
	}
	// Keep -c as a single Lua chunk; lang is validated above.
	script := fmt.Sprintf(
		`lua local f = vim.api.nvim_get_runtime_file("queries/%s/highlights.scm", false); io.stdout:write((f and #f > 0) and "1" or "0")`,
		lang,
	)
	code, out, err := neovimShellOutCapture("nvim", []string{
		"--clean",
		"--headless",
		"-i", "NONE",
		"-c", script,
		"-c", "qa",
	}, "", nil)
	if err != nil {
		return false, fmt.Errorf("nvim query probe: %w", err)
	}
	if code != 0 {
		return false, fmt.Errorf("nvim query probe exited %d: %s", code, strings.TrimSpace(out))
	}
	return strings.TrimSpace(out) == "1", nil
}

func neovimForceInstallQueriesFromGrammars() bool {
	v := strings.ToLower(strings.TrimSpace(neovimGetenv("NVPM_NEOVIM_ALWAYS_INSTALL_QUERIES")))
	return v == "1" || v == "true" || v == "yes"
}

func detectNeovimDataPath() (string, error) {
	// Prefer Neovim itself: this respects OS + user overrides.
	if code, out, err := neovimShellOutCapture("nvim", []string{
		"--headless",
		"+lua io.write(vim.fn.stdpath('data'))",
		"+q",
	}, "", nil); err == nil && code == 0 {
		p := strings.TrimSpace(out)
		if p != "" {
			return p, nil
		}
	}

	home, err := neovimUserHomeDir()
	if err != nil {
		return "", err
	}

	switch runtime.GOOS {
	case "linux":
		if xdg := strings.TrimSpace(neovimGetenv("XDG_DATA_HOME")); xdg != "" {
			return filepath.Join(xdg, "nvim"), nil
		}
		return filepath.Join(home, ".local", "share", "nvim"), nil
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "nvim"), nil
	case "windows":
		// Neovim uses $LOCALAPPDATA/nvim-data by default.
		if local := strings.TrimSpace(neovimGetenv("LOCALAPPDATA")); local != "" {
			return filepath.Join(local, "nvim-data"), nil
		}
		// Fallback: best-effort.
		return filepath.Join(home, "AppData", "Local", "nvim-data"), nil
	default:
		// Best-effort fallback.
		return filepath.Join(home, ".local", "share", "nvim"), nil
	}
}

func installNeovimParsersFromCache(sourceID, version string, languages []string) error {
	return installNeovimParsersAndQueriesFromCache(sourceID, version, languages, nil)
}

func installNeovimParsersAndQueriesFromCache(sourceID, version string, languages []string, allowMissingParser map[string]struct{}) error {
	if !integrationEnabled("neovim") {
		return nil
	}
	dataDir, err := detectNeovimDataPath()
	if err != nil {
		return fmt.Errorf("detect neovim data path: %w", err)
	}
	destDir := filepath.Join(dataDir, "site", "parser")
	if err := neovimMkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create neovim parser dir: %w", err)
	}

	queriesRoot := filepath.Join(dataDir, "site", "queries")

	for _, lang := range languages {
		lang = strings.TrimSpace(lang)
		if lang == "" {
			continue
		}
		_, parserOptional := allowMissingParser[lang]
		srcPath := TreeSitterArtifactPath(sourceID, version, lang)
		b, err := neovimReadFile(srcPath)
		if err != nil && !parserOptional {
			return fmt.Errorf("read built parser %s: %w", lang, err)
		}
		if err == nil {
			destPath := filepath.Join(destDir, lang+SharedLibExt())
			if err := neovimWriteFile(destPath, b, 0o755); err != nil {
				return fmt.Errorf("write neovim parser %s: %w", lang, err)
			}
		}

		cacheQueries := neovimTreeSitterQueriesCacheDir(sourceID, version, lang)
		if st, err := neovimStat(cacheQueries); err == nil && st.IsDir() {
			entries, rerr := os.ReadDir(cacheQueries)
			if rerr == nil && len(entries) > 0 {
				skipQueries := false
				if !neovimForceInstallQueriesFromGrammars() {
					bundled, qerr := neovimBundledQueriesPresent(lang)
					if qerr != nil {
						return fmt.Errorf("neovim bundled queries check for %s: %w", lang, qerr)
					}
					if bundled {
						skipQueries = true
						destQueries := filepath.Join(queriesRoot, lang)
						// Older NVPM installs may have copied grammar queries here; they take precedence
						// over $VIMRUNTIME and can break markdown (and other) treesitter in Neovim.
						if err := neovimRemoveAll(destQueries); err != nil {
							return fmt.Errorf("remove shadowing neovim queries %s: %w", lang, err)
						}
						AddIntegrationReportLine(sourceID, version, fmt.Sprintf(
							"Skipped Neovim site/queries install for %s (Neovim runtime already ships highlights; removed stale site/queries/%s if present; set NVPM_NEOVIM_ALWAYS_INSTALL_QUERIES=1 to force grammar queries)",
							lang, lang,
						))
					}
				}
				if !skipQueries {
					destQueries := filepath.Join(queriesRoot, lang)
					if err := neovimRemoveAll(destQueries); err != nil {
						return fmt.Errorf("remove stale neovim queries %s: %w", lang, err)
					}
					if err := copyNeovimTreeSitterQueriesDir(cacheQueries, destQueries, nil); err != nil {
						return fmt.Errorf("install neovim queries %s: %w", lang, err)
					}
				}
			}
		}
	}

	AddIntegrationReportLine(sourceID, version, fmt.Sprintf("Integrated into Neovim: parsers %s, queries %s", destDir, queriesRoot))
	return nil
}

// installedNeovimParserGrammarLanguages returns language names that have a parser shared library
// installed under Neovim's site/parser directory.
func installedNeovimParserGrammarLanguages() map[string]struct{} {
	out := map[string]struct{}{}
	if !integrationEnabled("neovim") {
		return out
	}
	dataDir, err := detectNeovimDataPath()
	if err != nil {
		return out
	}
	destDir := filepath.Join(dataDir, "site", "parser")
	entries, err := os.ReadDir(destDir)
	if err != nil {
		return out
	}
	ext := SharedLibExt()
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ext) {
			continue
		}
		lang := strings.ToLower(strings.TrimSuffix(name, ext))
		if lang != "" {
			out[lang] = struct{}{}
		}
	}
	return out
}

func parserLanguagesAvailableForNeovim(reg *registry_parser.RegistryParser, langs []string) map[string]struct{} {
	have := installedNeovimParserGrammarLanguages()
	if len(have) == 0 {
		have = installedParserGrammarLanguages(reg)
	}
	return have
}

func missingNeovimParserLanguages(reg *registry_parser.RegistryParser, langs []string) []string {
	have := parserLanguagesAvailableForNeovim(reg, langs)
	var missing []string
	for _, l := range langs {
		l = strings.ToLower(strings.TrimSpace(l))
		if l == "" {
			continue
		}
		if !treesitterdeps.LanguageNeedsNeovimParser(reg, l) {
			continue
		}
		if _, ok := have[l]; !ok {
			missing = append(missing, l)
		}
	}
	return missing
}

func installedTreeSitterGrammarLanguages(reg *registry_parser.RegistryParser) map[string]struct{} {
	out := map[string]struct{}{}
	for _, pkg := range local_packages_parser.GetData(false).Packages {
		item := reg.GetBySourceId(pkg.SourceID)
		if item.Source.ID == "" || !IsTreeSitterCategory(item.Categories) {
			continue
		}
		for _, l := range item.Languages {
			if s := strings.ToLower(strings.TrimSpace(l)); s != "" {
				out[s] = struct{}{}
			}
		}
		if item.TreeSitter != nil {
			for _, b := range item.TreeSitter.Build {
				if s := strings.ToLower(strings.TrimSpace(b.Language)); s != "" {
					out[s] = struct{}{}
				}
			}
		}
	}
	return out
}

func registrySourceIDsForTreeSitterLanguage(lang string, reg *registry_parser.RegistryParser) []string {
	return treesitterdeps.ParserCandidates(reg, lang)
}

func sortedLanguageKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func installRegistryTreeSitterPackagesForLanguages(
	consumerSourceID string,
	missing []string,
	reg *registry_parser.RegistryParser,
	hint string,
) error {
	return installRegistryTreeSitterPackagesInLanguageOrder(consumerSourceID, missing, reg, hint)
}

// installRegistryTreeSitterPackagesInLanguageOrder installs parser packages for languages in the
// given order (e.g. topological order from a requires graph). Duplicate source ids are skipped.
func installRegistryTreeSitterPackagesInLanguageOrder(
	consumerSourceID string,
	langsInOrder []string,
	reg *registry_parser.RegistryParser,
	hint string,
) error {
	var noRegistry []string
	seenInstall := map[string]struct{}{}
	for _, lang := range langsInOrder {
		lang = strings.ToLower(strings.TrimSpace(lang))
		if lang == "" || !treesitterdeps.LanguageNeedsNeovimParser(reg, lang) {
			continue
		}
		have := parserLanguagesAvailableForNeovim(reg, langsInOrder)
		if _, ok := have[lang]; ok {
			continue
		}
		id, err := resolveParserSourceIDForLanguage(lang, reg, consumerSourceID, "")
		if err != nil {
			return err
		}
		if id == "" {
			noRegistry = append(noRegistry, lang)
			continue
		}
		if _, dup := seenInstall[id]; dup {
			continue
		}
		ver := strings.TrimSpace(reg.GetLatestVersion(id))
		dispVer := ver
		if dispVer == "" {
			dispVer = "latest"
		}
		title := fmt.Sprintf("Installing tree-sitter grammar %s@%s (%s)...", id, dispVer, lang)

		var installFailed bool
		action := func() {
			if !Install(id, ver) {
				installFailed = true
			}
		}

		if err := spinnerutil.RunWithTTYOrPlain(title, func() {
			if NeovimInheritInstallNotifierStart != nil {
				NeovimInheritInstallNotifierStart(id, ver)
			}
		}, action); err != nil {
			return err
		}

		if installFailed {
			return fmt.Errorf("failed to install tree-sitter grammar %s for language %q", id, lang)
		}
		item := reg.GetBySourceId(id)
		if item.Source.ID != "" {
			if err := ensureTreeSitterParserRequirements(item, ver); err != nil {
				return err
			}
		}
		noteTreeSitterDependencyInstallSuccess()
		instVer := strings.TrimSpace(local_packages_parser.GetBySourceId(id).Version)
		if instVer == "" {
			instVer = ver
		}
		if NeovimInheritInstallNotifierDone != nil {
			NeovimInheritInstallNotifierDone(id, instVer)
		}
		seenInstall[id] = struct{}{}
	}
	if len(seenInstall) == 0 && len(noRegistry) > 0 {
		return fmt.Errorf(
			"no registry Tree-sitter-parser packages found for languages: %s%s",
			strings.Join(noRegistry, ", "),
			hint,
		)
	}
	still := missingNeovimParserLanguages(reg, langsInOrder)
	if len(still) > 0 {
		msg := fmt.Sprintf(
			"after installing dependencies, still missing tree-sitter grammar(s): %s",
			strings.Join(still, ", "),
		)
		if len(noRegistry) > 0 {
			msg += fmt.Sprintf("; no registry package for: %s", strings.Join(noRegistry, ", "))
		}
		return fmt.Errorf("%s%s", msg, hint)
	}
	return nil
}

// neovimInheritContinueAnywaySourceID is set when the user chooses "Continue anyway" during
// PreflightNeovimTreeSitterInheritDeps so buildAndMaybeIntegrateTreeSitter does not prompt a second time.
var neovimInheritContinueAnywaySourceID string

// PreflightNeovimTreeSitterInheritDeps runs inherit dependency checks (prompt / nested install)
// before the CLI install spinner so prompts are not shown while the spinner claims work is in progress.
// It is a no-op unless Neovim integration is enabled and the registry item declares inherits.
func PreflightNeovimTreeSitterInheritDeps(registryItem registry_parser.RegistryItem) error {
	if registryItem.Source.ID == "" {
		return nil
	}
	if !integrationEnabled("neovim") {
		return nil
	}
	if !IsTreeSitterCategory(registryItem.Categories) {
		return nil
	}
	if registryItem.TreeSitter == nil {
		return nil
	}
	hasInherits := false
	for _, b := range registryItem.TreeSitter.Build {
		if len(b.Inherits) > 0 && TreeSitterBuildDeclaresNeovimIntegration(b) {
			hasInherits = true
			break
		}
	}
	if !hasInherits {
		return nil
	}
	if neovimInheritContinueAnywaySourceID != "" &&
		neovimInheritContinueAnywaySourceID != registryItem.Source.ID {
		neovimInheritContinueAnywaySourceID = ""
	}
	return resolveNeovimTreeSitterInheritDependencies(registryItem, func() {
		neovimInheritContinueAnywaySourceID = registryItem.Source.ID
	})
}

func resolveNeovimTreeSitterInheritDependencies(
	registryItem registry_parser.RegistryItem,
	onContinueAnyway func(),
) error {
	if !integrationEnabled("neovim") {
		return nil
	}
	if registryItem.TreeSitter == nil {
		return nil
	}
	need := map[string]struct{}{}
	for _, b := range registryItem.TreeSitter.Build {
		if !TreeSitterBuildDeclaresNeovimIntegration(b) {
			continue
		}
		for _, in := range b.Inherits {
			if s := strings.ToLower(strings.TrimSpace(in)); s != "" {
				need[s] = struct{}{}
			}
		}
	}
	if len(need) == 0 {
		return nil
	}
	reg := registry_parser.NewDefaultRegistryParser()
	needList := sortedLanguageKeys(need)
	missing := missingNeovimParserLanguages(reg, needList)
	if len(missing) == 0 {
		if registryItem.Source.ID == neovimInheritContinueAnywaySourceID {
			neovimInheritContinueAnywaySourceID = ""
		}
		return nil
	}
	sort.Strings(missing)
	var hint strings.Builder
	for _, l := range missing {
		if ids := registrySourceIDsForTreeSitterLanguage(l, reg); len(ids) > 0 {
			fmt.Fprintf(&hint, "\n• %s - e.g. nvpm install %s --integrate neovim", l, ids[0])
		} else {
			fmt.Fprintf(&hint, "\n• %s - install a Tree-sitter-parser package that lists this language in the registry", l)
		}
	}
	title := fmt.Sprintf("Missing base tree-sitter grammar(s) for Neovim: %s", strings.Join(missing, ", "))
	desc := "Queries may not resolve inherited captures until these are installed via NVPM (Tree-sitter-parser packages whose languages include the names above)." + hint.String()
	action, err := neovimInheritsPrompt(title, desc)
	if err != nil {
		return err
	}
	switch action {
	case neovimInheritsAbort:
		return fmt.Errorf("aborted: install inherited grammar(s) first%s", hint.String())
	case neovimInheritsContinue:
		if onContinueAnyway != nil {
			onContinueAnyway()
		}
		return nil
	case neovimInheritsInstall:
		// Do not carry over a prior "continue anyway" skip from an aborted install attempt.
		neovimInheritContinueAnywaySourceID = ""
		return installRegistryTreeSitterPackagesForLanguages(registryItem.Source.ID, missing, reg, hint.String())
	default:
		return fmt.Errorf("aborted: install inherited grammar(s) first%s", hint.String())
	}
}

func ensureNeovimTreeSitterInheritDependencies(registryItem registry_parser.RegistryItem) error {
	if registryItem.Source.ID != "" &&
		neovimInheritContinueAnywaySourceID == registryItem.Source.ID {
		neovimInheritContinueAnywaySourceID = ""
		return nil
	}
	return resolveNeovimTreeSitterInheritDependencies(registryItem, nil)
}

// buildTreeSitterOpts configures buildAndMaybeIntegrateTreeSitter for phased GitHub installs.
type buildTreeSitterOpts struct {
	SkipInheritDependencies bool
	// ExternalQueryPreflight, when non-nil, skips the external-query batch confirm in the cache step
	// and applies AllowUnpinned only to languages without a matching lock pin.
	ExternalQueryPreflight *ExternalQueryPreflightChoice
}

func queryOnlyNeovimLanguagesForInstall(build []registry_parser.RegistryItemTreeSitterBuild, langs []string) map[string]struct{} {
	want := map[string]struct{}{}
	for _, l := range langs {
		if s := strings.TrimSpace(l); s != "" {
			want[s] = struct{}{}
		}
	}
	out := map[string]struct{}{}
	for _, b := range build {
		lang := strings.TrimSpace(b.Language)
		if lang == "" || !b.QueriesOnly || !TreeSitterBuildDeclaresNeovimIntegration(b) {
			continue
		}
		if _, ok := want[lang]; ok {
			out[lang] = struct{}{}
		}
	}
	return out
}

func buildAndMaybeIntegrateTreeSitter(repoPath string, registryItem registry_parser.RegistryItem, version string, opts *buildTreeSitterOpts) ([]local_packages_parser.TreeSitterExternalQueryPin, error) {
	if !IsTreeSitterCategory(registryItem.Categories) {
		return nil, nil
	}
	if registryItem.TreeSitter == nil || len(registryItem.TreeSitter.Build) == 0 {
		return nil, nil
	}

	if err := ensureTreeSitterParserRequirements(registryItem, version); err != nil {
		return nil, err
	}
	if opts == nil || !opts.SkipInheritDependencies {
		if err := ensureNeovimTreeSitterInheritDependencies(registryItem); err != nil {
			return nil, err
		}
	}
	if err := ensureNeovimTreeSitterInjectionQueryPackages(registryItem, version); err != nil {
		return nil, err
	}

	langs, err := BuildTreeSitterParsersToCache(repoPath, registryItem.Source.ID, version, registryItem.TreeSitter.Build)
	if err != nil {
		return nil, err
	}
	var pre *ExternalQueryPreflightChoice
	if opts != nil {
		pre = opts.ExternalQueryPreflight
	}
	pins, err := cacheNeovimTreeSitterQueriesForBuiltLangs(repoPath, registryItem.Source.ID, version, registryItem.TreeSitter.Build, langs, pre)
	if err != nil {
		return nil, err
	}
	neovimLangs := FilterLanguagesForNeovimTreeSitterIntegration(registryItem.TreeSitter.Build, langs)
	queryOnly := queryOnlyNeovimLanguagesForInstall(registryItem.TreeSitter.Build, neovimLangs)
	if err := installNeovimParsersAndQueriesFromCache(registryItem.Source.ID, version, neovimLangs, queryOnly); err != nil {
		return nil, err
	}
	return pins, nil
}

func removeNeovimTreeSitterParsers(registryItem registry_parser.RegistryItem) error {
	if !integrationEnabled("neovim") {
		return nil
	}
	if !IsTreeSitterCategory(registryItem.Categories) {
		return nil
	}
	if registryItem.TreeSitter == nil {
		return nil
	}
	if len(registryItem.TreeSitter.Build) == 0 {
		return nil
	}

	dataDir, err := detectNeovimDataPath()
	if err != nil {
		return fmt.Errorf("detect neovim data path: %w", err)
	}

	destDir := filepath.Join(dataDir, "site", "parser")
	exts := []string{".so", ".dylib", ".dll"}

	langs := map[string]struct{}{}
	for _, b := range registryItem.TreeSitter.Build {
		if !TreeSitterBuildDeclaresNeovimIntegration(b) {
			continue
		}
		if strings.TrimSpace(b.Language) != "" {
			langs[strings.TrimSpace(b.Language)] = struct{}{}
		}
	}

	queriesDir := filepath.Join(dataDir, "site", "queries")

	for lang := range langs {
		lang = strings.TrimSpace(lang)
		if lang == "" {
			continue
		}
		for _, ext := range exts {
			_ = neovimRemove(filepath.Join(destDir, lang+ext))
		}
		_ = neovimRemoveAll(filepath.Join(queriesDir, lang))
	}
	return nil
}

package providers

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mattn/go-isatty"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/spinnerutil"
	"github.com/mistweaverco/nvpm-client/internal/lib/treesitterdeps"
)

// PreflightTreeSitterParserRequirements installs missing parser-grammar dependencies declared via
// treesitter.build[].requires before clone/build or Neovim inherit preflights run.
// resolvedVersion may be empty; registry latest is used for new lock rows when disambiguating parsers.
func PreflightTreeSitterParserRequirements(registryItem registry_parser.RegistryItem, resolvedVersion string) error {
	return ensureTreeSitterParserRequirements(registryItem, resolvedVersion)
}

func ensureTreeSitterParserRequirements(registryItem registry_parser.RegistryItem, resolvedVersion string) error {
	if registryItem.Source.ID == "" || registryItem.TreeSitter == nil {
		return nil
	}
	if !IsTreeSitterCategory(registryItem.Categories) {
		return nil
	}
	reg := registry_parser.NewDefaultRegistryParser()
	rootLangs := treesitterdeps.RootParserLanguages(registryItem)
	if len(rootLangs) == 0 {
		return nil
	}
	resolve := func(lang string) (string, error) {
		return resolveParserSourceIDForLanguage(lang, reg, registryItem.Source.ID, resolvedVersion)
	}
	edges, err := treesitterdeps.BuildParserRequireEdges(registryItem, reg, resolve)
	if err != nil {
		return err
	}
	if len(edges) == 0 {
		return nil
	}
	order, err := treesitterdeps.TopoInstallOrder(rootLangs, edges)
	if err != nil {
		return err
	}
	rootProvides := map[string]struct{}{}
	for _, b := range registryItem.TreeSitter.Build {
		if b.QueriesOnly {
			continue
		}
		if strings.TrimSpace(b.GrammarDir) == "" {
			continue
		}
		if lg := strings.ToLower(strings.TrimSpace(b.Language)); lg != "" {
			rootProvides[lg] = struct{}{}
		}
	}
	have := installedParserGrammarLanguages(reg)
	for _, lang := range order {
		lang = strings.ToLower(strings.TrimSpace(lang))
		if lang == "" {
			continue
		}
		if _, skip := rootProvides[lang]; skip {
			continue
		}
		if _, ok := have[lang]; ok {
			continue
		}
		sourceID, err := resolveParserSourceIDForLanguage(lang, reg, registryItem.Source.ID, resolvedVersion)
		if err != nil {
			return err
		}
		ver := strings.TrimSpace(reg.GetLatestVersion(sourceID))
		if ver == "" {
			ver = "latest"
		}
		title := fmt.Sprintf("Installing parser dependency %s@%s (%s)...", sourceID, ver, lang)
		var installFailed bool
		action := func() {
			if !Install(sourceID, ver) {
				installFailed = true
			}
		}
		if err := spinnerutil.RunWithTTYOrPlain(title, func() {
			if NeovimInheritInstallNotifierStart != nil {
				NeovimInheritInstallNotifierStart(sourceID, ver)
			}
		}, action); err != nil {
			return err
		}
		if installFailed {
			return fmt.Errorf("failed to install parser dependency %s for language %q", sourceID, lang)
		}
		noteTreeSitterDependencyInstallSuccess()
		instVer := strings.TrimSpace(local_packages_parser.GetBySourceId(sourceID).Version)
		if instVer == "" {
			instVer = ver
		}
		if NeovimInheritInstallNotifierDone != nil {
			NeovimInheritInstallNotifierDone(sourceID, instVer)
		}
		have = installedParserGrammarLanguages(reg)
	}
	return nil
}

func installedParserGrammarLanguages(reg *registry_parser.RegistryParser) map[string]struct{} {
	out := map[string]struct{}{}
	for _, pkg := range local_packages_parser.GetData(false).Packages {
		item := reg.GetBySourceId(pkg.SourceID)
		if item.Source.ID == "" || !treesitterdeps.IsTreeSitterParserPackage(item.Categories) {
			continue
		}
		if treesitterdeps.IsTreeSitterQueriesPackage(item.Categories) {
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

func resolveParserSourceIDForLanguage(lang string, reg *registry_parser.RegistryParser, consumerSourceID, consumerResolvedVersion string) (string, error) {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" {
		return "", fmt.Errorf("empty language")
	}
	consumerSourceID = strings.TrimSpace(consumerSourceID)
	if lockID, ok := local_packages_parser.GetTreeSitterParserLockChoice(consumerSourceID, lang); ok && lockID != "" {
		item := reg.GetBySourceId(lockID)
		if item.Source.ID != "" && treesitterdeps.IsTreeSitterParserPackage(item.Categories) {
			return lockID, nil
		}
	}
	cands := treesitterdeps.ParserCandidates(reg, lang)
	if len(cands) == 0 {
		return "", fmt.Errorf("no Tree-sitter-parser registry package for language %q", lang)
	}
	if len(cands) == 1 {
		return cands[0], nil
	}
	sort.Strings(cands)
	title := fmt.Sprintf("Multiple registry parsers provide language %q", lang)
	desc := fmt.Sprintf("Choose which package to use when resolving dependencies for %s.\n\nThis choice is saved in nvpm-lock.json for this package.", consumerSourceID)
	if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stderr.Fd()) {
		return "", fmt.Errorf("%s\n%s\n\nNon-interactive session: add extras.treesitter_parser_choices to the lock row for %s with {\"language\":%q,\"sourceId\":\"...\"} (candidates: %s)",
			title, desc, consumerSourceID, lang, strings.Join(cands, ", "))
	}
	var chosen string
	opts := make([]huh.Option[string], 0, len(cands))
	for _, id := range cands {
		opts = append(opts, huh.NewOption(id, id))
	}
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(title).
				Description(desc).
				Options(opts...).
				Value(&chosen),
		),
	)
	if err := form.Run(); err != nil {
		return "", err
	}
	chosen = strings.TrimSpace(chosen)
	if chosen == "" {
		return "", fmt.Errorf("no parser package selected for language %q", lang)
	}
	if err := local_packages_parser.MergePackageTreeSitterParserChoice(consumerSourceID, lang, chosen, lockVersionForParserChoice(reg, consumerSourceID, consumerResolvedVersion)); err != nil {
		return "", err
	}
	return chosen, nil
}

func lockVersionForParserChoice(reg *registry_parser.RegistryParser, consumerSourceID, explicit string) string {
	v := strings.TrimSpace(explicit)
	if v != "" {
		return v
	}
	if v = strings.TrimSpace(local_packages_parser.GetBySourceId(consumerSourceID).Version); v != "" {
		return v
	}
	return strings.TrimSpace(reg.GetLatestVersion(consumerSourceID))
}

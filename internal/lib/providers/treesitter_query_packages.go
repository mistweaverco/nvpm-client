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
	"github.com/mistweaverco/nvpm-client/internal/lib/treesitterdeps"
)

// PreflightTreeSitterInjectionQueryPackages resolves Neovim injection host languages declared on
// treesitter.build[].injections: builds a parser requires graph for each host grammar, prompts when
// grammars are missing, and installs Tree-sitter-parser packages in topological order.
func PreflightTreeSitterInjectionQueryPackages(registryItem registry_parser.RegistryItem, resolvedVersion string) error {
	return resolveNeovimTreeSitterInjectionDependencies(registryItem, resolvedVersion, func() {
		neovimInjectionContinueAnywaySourceID = registryItem.Source.ID
	})
}

func ensureNeovimTreeSitterInjectionQueryPackages(registryItem registry_parser.RegistryItem, resolvedVersion string) error {
	if registryItem.Source.ID != "" &&
		neovimInjectionContinueAnywaySourceID == registryItem.Source.ID {
		neovimInjectionContinueAnywaySourceID = ""
		return nil
	}
	return resolveNeovimTreeSitterInjectionDependencies(registryItem, resolvedVersion, nil)
}

// neovimInjectionContinueAnywaySourceID is set when the user chooses "Continue anyway" during
// injection preflight so buildAndMaybeIntegrateTreeSitter does not prompt a second time.
var neovimInjectionContinueAnywaySourceID string

func resolveNeovimTreeSitterInjectionDependencies(
	registryItem registry_parser.RegistryItem,
	resolvedVersion string,
	onContinueAnyway func(),
) error {
	if registryItem.Source.ID == "" || registryItem.TreeSitter == nil {
		return nil
	}
	if !IsTreeSitterCategory(registryItem.Categories) {
		return nil
	}
	if !integrationEnabled("neovim") {
		return nil
	}
	langs := treesitterdeps.MergeInjectionLanguagesForEditor(registryItem, "neovim")
	if len(langs) == 0 {
		return nil
	}
	reg := registry_parser.NewDefaultRegistryParser()
	resolve := func(lang string) (string, error) {
		id, err := resolveParserSourceIDForLanguage(lang, reg, registryItem.Source.ID, resolvedVersion)
		if err != nil && strings.Contains(err.Error(), "no Tree-sitter-parser registry package") {
			return "", nil
		}
		return id, err
	}
	edges, injectionLangs, err := treesitterdeps.BuildInjectionParserRequireEdges(registryItem, reg, "neovim", resolve)
	if err != nil {
		return err
	}
	order, err := treesitterdeps.TopoInstallOrder(injectionLangs, edges)
	if err != nil {
		return err
	}
	missing := missingNeovimParserLanguages(reg, order)
	if len(missing) == 0 {
		if registryItem.Source.ID == neovimInjectionContinueAnywaySourceID {
			neovimInjectionContinueAnywaySourceID = ""
		}
		return nil
	}
	var hint strings.Builder
	for _, l := range missing {
		if ids := registrySourceIDsForTreeSitterLanguage(l, reg); len(ids) > 0 {
			fmt.Fprintf(&hint, "\n• %s - e.g. nvpm install %s --integrate neovim", l, ids[0])
		} else {
			fmt.Fprintf(&hint, "\n• %s - install a Tree-sitter-parser package that lists this language in the registry", l)
		}
	}
	title := fmt.Sprintf("Missing tree-sitter grammar(s) for Neovim injections: %s", strings.Join(missing, ", "))
	desc := "Injected regions in this grammar need these host parsers installed via NVPM (Tree-sitter-parser packages whose languages include the names above)." + hint.String()
	action, err := neovimInheritsPrompt(title, desc)
	if err != nil {
		return err
	}
	switch action {
	case neovimInheritsAbort:
		return fmt.Errorf("aborted: install injection host grammar(s) first%s", hint.String())
	case neovimInheritsContinue:
		if onContinueAnyway != nil {
			onContinueAnyway()
		}
		return nil
	case neovimInheritsInstall:
		neovimInjectionContinueAnywaySourceID = ""
		return installRegistryTreeSitterPackagesInLanguageOrder(registryItem.Source.ID, missing, reg, hint.String())
	default:
		return fmt.Errorf("aborted: install injection host grammar(s) first%s", hint.String())
	}
}

func resolveQueryPackageSourceIDForLanguage(
	lang, integration string,
	reg *registry_parser.RegistryParser,
	consumerSourceID, consumerResolvedVersion string,
) (string, error) {
	lang = strings.ToLower(strings.TrimSpace(lang))
	integration = strings.ToLower(strings.TrimSpace(integration))
	if lang == "" || integration == "" {
		return "", fmt.Errorf("empty language or integration")
	}
	consumerSourceID = strings.TrimSpace(consumerSourceID)
	if lockID, ok := local_packages_parser.GetTreeSitterQueryLockChoice(consumerSourceID, lang, integration); ok && lockID != "" {
		item := reg.GetBySourceId(lockID)
		if item.Source.ID != "" && treesitterdeps.IsTreeSitterQueriesPackage(item.Categories) {
			return lockID, nil
		}
	}
	cands := treesitterdeps.QueryPackageCandidates(reg, lang, integration)
	if len(cands) == 0 {
		return "", nil
	}
	if len(cands) == 1 {
		return cands[0], nil
	}
	sort.Strings(cands)
	title := fmt.Sprintf("Multiple Tree-sitter-queries packages for %q (%s)", lang, integration)
	desc := fmt.Sprintf(
		"Choose which registry package supplies Neovim queries for this language when installing %s.\n\nThis choice is saved in nvpm-lock.json.",
		consumerSourceID,
	)
	if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stderr.Fd()) {
		return "", fmt.Errorf(
			"%s\n%s\n\nNon-interactive session: add extras.treesitter_query_choices to the lock row for %s with {\"language\":%q,\"integration\":%q,\"sourceId\":\"...\"} (candidates: %s)",
			title, desc, consumerSourceID, lang, integration, strings.Join(cands, ", "),
		)
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
		return "", fmt.Errorf("no Tree-sitter-queries package selected for language %q", lang)
	}
	ver := lockVersionForQueryChoice(reg, consumerSourceID, consumerResolvedVersion)
	if err := local_packages_parser.MergePackageTreeSitterQueryChoice(consumerSourceID, lang, integration, chosen, ver); err != nil {
		return "", err
	}
	return chosen, nil
}

func lockVersionForQueryChoice(reg *registry_parser.RegistryParser, consumerSourceID, explicit string) string {
	v := strings.TrimSpace(explicit)
	if v != "" {
		return v
	}
	if v = strings.TrimSpace(local_packages_parser.GetBySourceId(consumerSourceID).Version); v != "" {
		return v
	}
	return strings.TrimSpace(reg.GetLatestVersion(consumerSourceID))
}

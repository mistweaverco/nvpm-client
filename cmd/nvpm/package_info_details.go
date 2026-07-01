package nvpm

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/providers"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
)

type packageRequiresDetails struct {
	All          []string
	One          []string
	OneSatisfied []string
	AllInstalled []string
	AllMissing   []string
	OneMissing   bool
}

type packageExternalQueryDetails struct {
	RepoURL string
	Ref     string
	Semver  bool
}

type packageTreeSitterBuildDetails struct {
	Language        string
	GrammarDir      string
	QueriesOnly     bool
	Integrations    []string
	Inherits        []string
	ExternalQueries []packageExternalQueryDetails
}

type packageTreeSitterDetails struct {
	Languages    []string
	Integrations []string
	Build        []packageTreeSitterBuildDetails
}

type packageExtraDetails struct {
	Requires   *packageRequiresDetails
	TreeSitter *packageTreeSitterDetails
}

func isTreeSitterParserPackage(item registry_parser.RegistryItem) bool {
	return providers.IsTreeSitterCategory(item.Categories) && item.TreeSitter != nil && len(item.TreeSitter.Build) > 0
}

func collectPackageExtraDetails(item registry_parser.RegistryItem) packageExtraDetails {
	out := packageExtraDetails{}
	if item.Requires != nil && !item.Requires.IsEmpty() {
		out.Requires = collectRequiresDetails(item.Requires)
	}
	if isTreeSitterParserPackage(item) {
		out.TreeSitter = collectTreeSitterDetails(item)
	}
	return out
}

func collectRequiresDetails(req *registry_parser.RegistryItemRequires) *packageRequiresDetails {
	d := &packageRequiresDetails{
		All: append([]string(nil), req.All...),
		One: append([]string(nil), req.One...),
	}
	for _, ref := range req.All {
		id := normalizeInfoPackageRef(ref)
		if packageIsInstalled(id) {
			d.AllInstalled = append(d.AllInstalled, id)
		} else {
			d.AllMissing = append(d.AllMissing, id)
		}
	}
	for _, ref := range req.One {
		id := normalizeInfoPackageRef(ref)
		if packageIsInstalled(id) {
			d.OneSatisfied = append(d.OneSatisfied, id)
		}
	}
	d.OneMissing = len(req.One) > 0 && len(d.OneSatisfied) == 0
	sort.Strings(d.All)
	sort.Strings(d.One)
	sort.Strings(d.AllInstalled)
	sort.Strings(d.AllMissing)
	sort.Strings(d.OneSatisfied)
	return d
}

func collectTreeSitterDetails(item registry_parser.RegistryItem) *packageTreeSitterDetails {
	langSeen := map[string]struct{}{}
	intSeen := map[string]struct{}{}
	var langs []string
	var integrations []string
	var builds []packageTreeSitterBuildDetails

	for _, b := range item.TreeSitter.Build {
		lang := strings.TrimSpace(b.Language)
		if lang != "" {
			if _, ok := langSeen[lang]; !ok {
				langSeen[lang] = struct{}{}
				langs = append(langs, lang)
			}
		}
		row := packageTreeSitterBuildDetails{
			Language:    lang,
			GrammarDir:  strings.TrimSpace(b.GrammarDir),
			QueriesOnly: b.QueriesOnly,
			Inherits:    append([]string(nil), b.Inherits...),
		}
		for _, in := range b.Integrations {
			in = strings.TrimSpace(in)
			if in == "" {
				continue
			}
			row.Integrations = append(row.Integrations, in)
			if _, ok := intSeen[in]; !ok {
				intSeen[in] = struct{}{}
				integrations = append(integrations, in)
			}
		}
		sort.Strings(row.Integrations)
		sort.Strings(row.Inherits)
		for _, q := range b.ExternalQueries {
			if strings.TrimSpace(q.RepoURL) == "" {
				continue
			}
			row.ExternalQueries = append(row.ExternalQueries, packageExternalQueryDetails{
				RepoURL: strings.TrimSpace(q.RepoURL),
				Ref:     strings.TrimSpace(q.Ref),
				Semver:  q.Semver,
			})
		}
		builds = append(builds, row)
	}

	sort.Strings(langs)
	sort.Strings(integrations)
	return &packageTreeSitterDetails{
		Languages:    langs,
		Integrations: integrations,
		Build:        builds,
	}
}

func normalizeInfoPackageRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "pkg:") {
		legacy := strings.TrimPrefix(ref, "pkg:")
		parts := strings.SplitN(legacy, "/", 2)
		if len(parts) == 2 {
			return parts[0] + ":" + parts[1]
		}
	}
	if idx := strings.LastIndex(ref, "@"); idx > 0 {
		base := ref[:idx]
		if strings.Contains(base, ":") {
			return base
		}
	}
	return ref
}

var packageIsInstalled = local_packages_parser.IsPackageInstalled

func formatExternalQueryLine(q packageExternalQueryDetails) string {
	var tags []string
	if q.Semver {
		tags = append(tags, "semver")
	}
	if q.Ref != "" {
		tags = append(tags, "ref="+q.Ref)
	}
	if len(tags) == 0 {
		return q.RepoURL
	}
	return fmt.Sprintf("%s (%s)", q.RepoURL, strings.Join(tags, ", "))
}

func appendRequiresPlain(b *strings.Builder, req *packageRequiresDetails) {
	b.WriteString("Requires:\n")
	if len(req.All) > 0 {
		b.WriteString("  all (every package must be installed):\n")
		for _, id := range req.All {
			mark := requireInstallMark(id, req)
			b.WriteString(fmt.Sprintf("    %s %s\n", mark, id))
		}
	}
	if len(req.One) > 0 {
		b.WriteString("  one (at least one must be installed):\n")
		for _, id := range req.One {
			mark := requireInstallMark(id, req)
			b.WriteString(fmt.Sprintf("    %s %s\n", mark, id))
		}
	}
}

func requireInstallMark(id string, req *packageRequiresDetails) string {
	for _, s := range req.AllInstalled {
		if s == id {
			return "[installed]"
		}
	}
	for _, s := range req.OneSatisfied {
		if s == id {
			return "[installed]"
		}
	}
	return "[not installed]"
}

func appendRequiresMarkdown(b *strings.Builder, req *packageRequiresDetails) {
	b.WriteString("## Requires\n\n")
	if len(req.All) > 0 {
		b.WriteString("**All of** (every package must be installed):\n\n")
		for _, id := range req.All {
			b.WriteString(fmt.Sprintf("- %s `%s`\n", requireInstallMarkMarkdown(id, req), id))
		}
		b.WriteString("\n")
	}
	if len(req.One) > 0 {
		b.WriteString("**One of** (at least one must be installed):\n\n")
		for _, id := range req.One {
			b.WriteString(fmt.Sprintf("- %s `%s`\n", requireInstallMarkMarkdown(id, req), id))
		}
		b.WriteString("\n")
	}
}

func requireInstallMarkMarkdown(id string, req *packageRequiresDetails) string {
	for _, s := range req.AllInstalled {
		if s == id {
			return "✅"
		}
	}
	for _, s := range req.OneSatisfied {
		if s == id {
			return "✅"
		}
	}
	return "⬜"
}

func appendTreeSitterPlain(b *strings.Builder, ts *packageTreeSitterDetails) {
	b.WriteString("Tree-sitter:\n")
	if len(ts.Languages) > 0 {
		b.WriteString(fmt.Sprintf("  Languages: %s\n", strings.Join(ts.Languages, ", ")))
	}
	if len(ts.Integrations) > 0 {
		b.WriteString(fmt.Sprintf("  Integrations: %s\n", strings.Join(ts.Integrations, ", ")))
	}
	for _, row := range ts.Build {
		label := row.Language
		if label == "" {
			label = "(unknown)"
		}
		if row.QueriesOnly {
			label += " (queries only)"
		}
		b.WriteString(fmt.Sprintf("  Build %s:\n", label))
		if row.GrammarDir != "" && !row.QueriesOnly {
			b.WriteString(fmt.Sprintf("    Grammar directory: %s\n", row.GrammarDir))
		}
		if len(row.Integrations) > 0 {
			b.WriteString(fmt.Sprintf("    Integrations: %s\n", strings.Join(row.Integrations, ", ")))
		}
		if len(row.Inherits) > 0 {
			b.WriteString(fmt.Sprintf("    Inherits: %s\n", strings.Join(row.Inherits, ", ")))
		}
		if len(row.ExternalQueries) > 0 {
			b.WriteString("    External queries (optional):\n")
			for _, q := range row.ExternalQueries {
				b.WriteString(fmt.Sprintf("      - %s\n", formatExternalQueryLine(q)))
			}
		}
	}
}

func appendTreeSitterMarkdown(b *strings.Builder, ts *packageTreeSitterDetails) {
	b.WriteString("## Tree-sitter\n\n")
	if len(ts.Languages) > 0 {
		b.WriteString(fmt.Sprintf("**Languages installed:** %s\n\n", strings.Join(ts.Languages, ", ")))
	}
	if len(ts.Integrations) > 0 {
		b.WriteString(fmt.Sprintf("**Supported integrations:** %s\n\n", strings.Join(ts.Integrations, ", ")))
	}
	for _, row := range ts.Build {
		title := row.Language
		if title == "" {
			title = "build"
		}
		if row.QueriesOnly {
			title += " (queries only)"
		}
		b.WriteString(fmt.Sprintf("### %s\n\n", title))
		if row.GrammarDir != "" && !row.QueriesOnly {
			b.WriteString(fmt.Sprintf("- **Grammar directory:** `%s`\n", row.GrammarDir))
		}
		if len(row.Integrations) > 0 {
			b.WriteString(fmt.Sprintf("- **Integrations:** %s\n", strings.Join(row.Integrations, ", ")))
		}
		if len(row.Inherits) > 0 {
			b.WriteString(fmt.Sprintf("- **Inherits:** %s\n", strings.Join(row.Inherits, ", ")))
		}
		if len(row.ExternalQueries) > 0 {
			b.WriteString("- **External queries (optional):**\n")
			for _, q := range row.ExternalQueries {
				b.WriteString(fmt.Sprintf("  - %s\n", formatExternalQueryLine(q)))
			}
		}
		b.WriteString("\n")
	}
}

func requiresDetailsJSON(req *packageRequiresDetails) map[string]interface{} {
	out := map[string]interface{}{}
	if len(req.All) > 0 {
		out["all"] = req.All
		out["all_installed"] = req.AllInstalled
		out["all_missing"] = req.AllMissing
	}
	if len(req.One) > 0 {
		out["one"] = req.One
		out["one_satisfied"] = req.OneSatisfied
		out["one_missing"] = req.OneMissing
	}
	return out
}

func treeSitterDetailsJSON(ts *packageTreeSitterDetails) map[string]interface{} {
	builds := make([]map[string]interface{}, 0, len(ts.Build))
	for _, row := range ts.Build {
		entry := map[string]interface{}{
			"language": row.Language,
		}
		if row.GrammarDir != "" {
			entry["grammar_dir"] = row.GrammarDir
		}
		if row.QueriesOnly {
			entry["queries_only"] = true
		}
		if len(row.Integrations) > 0 {
			entry["integrations"] = row.Integrations
		}
		if len(row.Inherits) > 0 {
			entry["inherits"] = row.Inherits
		}
		if len(row.ExternalQueries) > 0 {
			queries := make([]map[string]interface{}, 0, len(row.ExternalQueries))
			for _, q := range row.ExternalQueries {
				qm := map[string]interface{}{"repo_url": q.RepoURL}
				if q.Ref != "" {
					qm["ref"] = q.Ref
				}
				if q.Semver {
					qm["semver"] = true
				}
				queries = append(queries, qm)
			}
			entry["external_queries"] = queries
		}
		builds = append(builds, entry)
	}
	out := map[string]interface{}{
		"build": builds,
	}
	if len(ts.Languages) > 0 {
		out["languages"] = ts.Languages
	}
	if len(ts.Integrations) > 0 {
		out["integrations"] = ts.Integrations
	}
	return out
}

func mergeExtraDetailsJSON(result map[string]interface{}, extra packageExtraDetails) {
	if extra.Requires != nil {
		result["requires"] = requiresDetailsJSON(extra.Requires)
	}
	if extra.TreeSitter != nil {
		result["treesitter"] = treeSitterDetailsJSON(extra.TreeSitter)
	}
}

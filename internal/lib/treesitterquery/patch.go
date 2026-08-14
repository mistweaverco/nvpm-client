package treesitterquery

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// QueryKindFromFilename derives QueryKind from a .scm basename.
func QueryKindFromFilename(path string) QueryKind {
	base := strings.ToLower(filepath.Base(path))
	if !strings.HasSuffix(base, ".scm") {
		return QueryKindUnknown
	}
	name := strings.TrimSuffix(base, ".scm")
	switch name {
	case "highlights":
		return QueryKindHighlights
	case "injections":
		return QueryKindInjections
	case "locals":
		return QueryKindLocals
	case "folds":
		return QueryKindFolds
	case "indents":
		return QueryKindIndents
	default:
		return QueryKindUnknown
	}
}

// Patch applies targeted Neovim compatibility transforms to a Tree-sitter query.
func Patch(content []byte, opts PatchOptions) (PatchResult, error) {
	tokens, err := Lex(content)
	if err != nil {
		return PatchResult{}, err
	}

	var edits []Edit
	var applied []AppliedPatch
	var diags []Diagnostic

	if opts.SourceDialect != DialectNeovim {
		exprs := discoverExpressions(tokens)
		e, a, d := applyPredicateRules(tokens, exprs)
		edits = append(edits, e...)
		applied = append(applied, a...)
		diags = append(diags, d...)
	}

	if inh := inheritsEdit(tokens, opts.Inherits); inh != nil {
		edits = append(edits, *inh)
		applied = append(applied, AppliedPatch{Rule: RuleInheritsModeline, Line: 1})
	}

	out, err := applyEdits(content, edits)
	if err != nil {
		return PatchResult{}, err
	}
	return PatchResult{Content: out, Applied: applied, Diagnostics: diags}, nil
}

func applyEdits(content []byte, edits []Edit) ([]byte, error) {
	if len(edits) == 0 {
		out := make([]byte, len(content))
		copy(out, content)
		return out, nil
	}
	sort.Slice(edits, func(i, j int) bool {
		if edits[i].Start != edits[j].Start {
			return edits[i].Start > edits[j].Start
		}
		return edits[i].End > edits[j].End
	})
	n := len(content)
	for i, e := range edits {
		if e.Start < 0 || e.End > n || e.Start > e.End {
			return nil, fmt.Errorf("invalid edit range [%d,%d) in %d-byte query", e.Start, e.End, n)
		}
		if i > 0 && e.End > edits[i-1].Start {
			return nil, fmt.Errorf("overlapping query edits at [%d,%d) and [%d,%d)", e.Start, e.End, edits[i-1].Start, edits[i-1].End)
		}
	}
	out := content
	for _, e := range edits {
		repl := []byte(e.Replacement)
		next := make([]byte, 0, len(out)-(e.End-e.Start)+len(repl))
		next = append(next, out[:e.Start]...)
		next = append(next, repl...)
		next = append(next, out[e.End:]...)
		out = next
	}
	return out, nil
}

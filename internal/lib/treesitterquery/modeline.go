package treesitterquery

import "strings"

func cleanInherits(inherits []string) []string {
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
	return clean
}

func hasInheritsModeline(tokens []Token) bool {
	nonEmpty := 0
	for _, tok := range tokens {
		switch tok.Kind {
		case TokenWhitespace:
			continue
		case TokenComment:
			nonEmpty++
			if nonEmpty > 8 {
				return false
			}
			if strings.Contains(strings.ToLower(tok.Raw), "inherits:") {
				return true
			}
		default:
			return false
		}
	}
	return false
}

func inheritsModelinePrefix(langs []string) string {
	return "; inherits: " + strings.Join(langs, ", ") + "\n"
}

func inheritsEdit(tokens []Token, inherits []string) *Edit {
	clean := cleanInherits(inherits)
	if len(clean) == 0 || hasInheritsModeline(tokens) {
		return nil
	}
	return &Edit{Start: 0, End: 0, Replacement: inheritsModelinePrefix(clean)}
}

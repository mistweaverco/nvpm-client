package treesitterquery

import (
	"fmt"
	"strings"
	"unicode"
)

type RegexTranslation struct {
	Regex       string
	Changed     bool
	Diagnostics []Diagnostic
}

func DecodeQueryString(raw string) (string, error) {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return "", fmt.Errorf("not a quoted query string")
	}
	inner := raw[1 : len(raw)-1]
	var b strings.Builder
	b.Grow(len(inner))
	for i := 0; i < len(inner); i++ {
		if inner[i] != '\\' {
			b.WriteByte(inner[i])
			continue
		}
		if i+1 >= len(inner) {
			return "", fmt.Errorf("trailing backslash in query string")
		}
		i++
		switch inner[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		default:
			b.WriteByte(inner[i])
		}
	}
	return b.String(), nil
}

func EncodeQueryString(value string) string {
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('"')
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteByte(value[i])
		}
	}
	b.WriteByte('"')
	return b.String()
}

func hasVimMagicPrefix(src string) bool {
	return strings.HasPrefix(src, `\v`) ||
		strings.HasPrefix(src, `\m`) ||
		strings.HasPrefix(src, `\M`) ||
		strings.HasPrefix(src, `\V`)
}

// TranslateTreeSitterRegexToVimVeryMagic converts a decoded Tree-sitter/PCRE-like
// regex into Vim very-magic syntax when a lossless translation is possible.
func TranslateTreeSitterRegexToVimVeryMagic(src string) (RegexTranslation, error) {
	if hasVimMagicPrefix(src) {
		return RegexTranslation{Regex: src}, nil
	}
	t := regexTranslator{src: src}
	body, needsVM, unsupported, err := t.translate()
	if err != nil {
		return RegexTranslation{}, err
	}
	if unsupported != "" {
		return RegexTranslation{
			Regex: src,
			Diagnostics: []Diagnostic{{
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("cannot safely translate regex construct %q; leaving regex unchanged", unsupported),
			}},
		}, nil
	}
	if !needsVM {
		return RegexTranslation{Regex: src}, nil
	}
	return RegexTranslation{Regex: `\v` + body, Changed: true}, nil
}

type regexTranslator struct {
	src string
	i   int
}

func (t *regexTranslator) translate() (body string, needsVM bool, unsupported string, err error) {
	var b strings.Builder
	for t.i < len(t.src) {
		chunk, vm, u, e := t.next()
		if e != nil {
			return "", false, "", e
		}
		if u != "" {
			return "", false, u, nil
		}
		b.WriteString(chunk)
		needsVM = needsVM || vm
	}
	return b.String(), needsVM, "", nil
}

func (t *regexTranslator) peek() byte {
	if t.i >= len(t.src) {
		return 0
	}
	return t.src[t.i]
}

func (t *regexTranslator) next() (chunk string, needsVM bool, unsupported string, err error) {
	c := t.src[t.i]
	switch c {
	case '\\':
		return t.escape()
	case '[':
		return t.class()
	case '(':
		return t.group()
	case ')':
		t.i++
		return ")", true, "", nil
	case '|':
		t.i++
		return "|", true, "", nil
	case '*', '+', '?':
		return t.quantifier()
	case '{':
		return t.brace()
	case '^', '$', '.':
		t.i++
		return string(c), false, "", nil
	default:
		t.i++
		return t.literal(c), false, "", nil
	}
}

func (t *regexTranslator) quantifier() (string, bool, string, error) {
	q := t.src[t.i]
	t.i++
	needsVM := q == '+' || q == '?'
	if t.peek() == '+' || t.peek() == '?' {
		mod := t.src[t.i]
		return "", false, string(q) + string(mod), nil
	}
	return string(q), needsVM, "", nil
}

func (t *regexTranslator) brace() (string, bool, string, error) {
	// {m}, {m,}, {m,n} or literal {
	if !t.looksLikeQuantifierBrace() {
		t.i++
		return t.literal('{'), false, "", nil
	}
	start := t.i
	t.i++ // {
	for t.i < len(t.src) && t.src[t.i] != '}' {
		t.i++
	}
	if t.i >= len(t.src) {
		return "", false, "{", nil
	}
	t.i++ // }
	body := t.src[start:t.i]
	if t.peek() == '+' || t.peek() == '?' {
		return "", false, body + string(t.peek()), nil
	}
	return body, true, "", nil
}

func (t *regexTranslator) looksLikeQuantifierBrace() bool {
	// { followed by digit
	if t.i+1 >= len(t.src) || !unicode.IsDigit(rune(t.src[t.i+1])) {
		return false
	}
	j := t.i + 1
	for j < len(t.src) && unicode.IsDigit(rune(t.src[j])) {
		j++
	}
	if j < len(t.src) && t.src[j] == ',' {
		j++
		for j < len(t.src) && unicode.IsDigit(rune(t.src[j])) {
			j++
		}
	}
	return j < len(t.src) && t.src[j] == '}'
}

func (t *regexTranslator) group() (string, bool, string, error) {
	if t.i+1 < len(t.src) && t.src[t.i+1] == '?' {
		return t.specialGroup()
	}
	t.i++
	return "(", true, "", nil
}

func (t *regexTranslator) specialGroup() (string, bool, string, error) {
	rest := t.src[t.i:]
	switch {
	case strings.HasPrefix(rest, "(?:"):
		t.i += 3
		return "%(", true, "", nil
	case strings.HasPrefix(rest, "(?="):
		return "", false, "(?=...)", nil
	case strings.HasPrefix(rest, "(?!"):
		return "", false, "(?!...)", nil
	case strings.HasPrefix(rest, "(?<="):
		return "", false, "(?<=...)", nil
	case strings.HasPrefix(rest, "(?<!"):
		return "", false, "(?<!...)", nil
	case strings.HasPrefix(rest, "(?>"):
		return "", false, "(?>...)", nil
	case strings.HasPrefix(rest, "(?P<"), strings.HasPrefix(rest, "(?<"):
		return "", false, "named capture", nil
	case strings.HasPrefix(rest, "(?i"), strings.HasPrefix(rest, "(?m"),
		strings.HasPrefix(rest, "(?s"), strings.HasPrefix(rest, "(?u"),
		strings.HasPrefix(rest, "(?x"):
		return "", false, "inline regex flags", nil
	default:
		return "", false, rest[:minInt(len(rest), 6)], nil
	}
}

func (t *regexTranslator) escape() (string, bool, string, error) {
	if t.i+1 >= len(t.src) {
		t.i++
		return `\\`, false, "", nil
	}
	e := t.src[t.i+1]
	switch e {
	case 'd':
		t.i += 2
		return "[0-9]", true, "", nil
	case 'D':
		t.i += 2
		return "[^0-9]", true, "", nil
	case 's':
		t.i += 2
		return `\_s`, true, "", nil
	case 'S':
		t.i += 2
		return `\_S`, true, "", nil
	case 'w':
		t.i += 2
		return "[0-9A-Za-z_]", true, "", nil
	case 'W':
		t.i += 2
		return "[^0-9A-Za-z_]", true, "", nil
	case 'n', 't', 'r':
		t.i += 2
		return `\` + string(e), false, "", nil
	case 'b', 'B':
		return "", false, `\` + string(e), nil
	case 'A', 'z', 'Z', 'G':
		return "", false, `\` + string(e), nil
	case 'k', 'K':
		return "", false, `\` + string(e), nil
	case 'p', 'P':
		return "", false, `\` + string(e) + `{...}`, nil
	case 'Q', 'E':
		return "", false, `\` + string(e), nil
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return "", false, "backreferences", nil
	default:
		t.i += 2
		// Keep escaped punctuation as an escaped literal in very-magic.
		return `\` + string(e), false, "", nil
	}
}

func (t *regexTranslator) class() (string, bool, string, error) {
	start := t.i
	t.i++ // [
	if t.i >= len(t.src) {
		return "", false, "[", nil
	}
	neg := false
	if t.src[t.i] == '^' {
		neg = true
		t.i++
	}
	// ] immediately after [ or [^ is literal
	var inner strings.Builder
	needsVM := false
	first := true
	for t.i < len(t.src) {
		c := t.src[t.i]
		if c == ']' && !first {
			t.i++
			out := "["
			if neg {
				out = "[^"
			}
			return out + inner.String() + "]", needsVM, "", nil
		}
		first = false
		if c == '\\' {
			if t.i+1 >= len(t.src) {
				return "", false, string(t.src[start:]), nil
			}
			e := t.src[t.i+1]
			switch e {
			case 'd':
				inner.WriteString("0-9")
				needsVM = true
				t.i += 2
			case 'D':
				return "", false, `[\D]`, nil
			case 'w':
				inner.WriteString("0-9A-Za-z_")
				needsVM = true
				t.i += 2
			case 'W':
				return "", false, `[\W]`, nil
			case 's':
				inner.WriteString(" \t\n\r")
				needsVM = true
				t.i += 2
			case 'S':
				return "", false, `[\S]`, nil
			case 'p', 'P':
				return "", false, "Unicode property classes", nil
			default:
				inner.WriteByte('\\')
				inner.WriteByte(e)
				t.i += 2
			}
			continue
		}
		if c == '[' && t.i+1 < len(t.src) && t.src[t.i+1] == ':' {
			// POSIX class [:name:] - copy through; Vim supports these.
			end := strings.Index(t.src[t.i+2:], ":]")
			if end < 0 {
				return "", false, "unterminated POSIX class", nil
			}
			inner.WriteString(t.src[t.i : t.i+2+end+2])
			t.i += 2 + end + 2
			continue
		}
		if c == '&' && t.i+1 < len(t.src) && t.src[t.i+1] == '&' {
			return "", false, "character class intersection", nil
		}
		inner.WriteByte(c)
		t.i++
	}
	return "", false, string(t.src[start:]), nil
}

func (t *regexTranslator) literal(c byte) string {
	// Characters that are extra-special in Vim very-magic but literal in PCRE.
	switch c {
	case '~', '&', '@', '=', '<', '>', '%':
		return `\` + string(c)
	}
	return string(c)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

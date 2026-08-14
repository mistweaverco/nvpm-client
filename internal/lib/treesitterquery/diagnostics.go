package treesitterquery

const (
	RuleInheritsModeline = "neovim/inherits-modeline"
	RuleRegexMatch       = "neovim/regex-match"
	RuleRegexNotMatch    = "neovim/regex-not-match"
	RuleRegexAnyMatch    = "neovim/regex-any-match"
	RuleRegexAnyNotMatch = "neovim/regex-any-not-match"
	RulePropertyIs       = "neovim/property-is"
	RulePropertyIsNot    = "neovim/property-is-not"
)

func loc(tok Token) (line, col int) {
	return tok.Line, tok.Column
}

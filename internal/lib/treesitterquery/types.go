package treesitterquery

// NeovimQueryPatchVersion identifies the semantic query patcher behavior.
// Increment when patch output changes so cached queries can be invalidated.
const NeovimQueryPatchVersion = 2

type Dialect string

const (
	DialectTreeSitter Dialect = "tree-sitter"
	DialectNeovim     Dialect = "neovim"
)

type QueryKind string

const (
	QueryKindUnknown    QueryKind = ""
	QueryKindHighlights QueryKind = "highlights"
	QueryKindInjections QueryKind = "injections"
	QueryKindLocals     QueryKind = "locals"
	QueryKindFolds      QueryKind = "folds"
	QueryKindIndents    QueryKind = "indents"
)

type PatchOptions struct {
	Language      string
	QueryKind     QueryKind
	SourceDialect Dialect
	TargetDialect Dialect
	Inherits      []string
}

type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

type Diagnostic struct {
	Severity Severity
	Rule     string
	Message  string
	Line     int
	Column   int
}

type AppliedPatch struct {
	Rule string
	Line int
}

type PatchResult struct {
	Content     []byte
	Applied     []AppliedPatch
	Diagnostics []Diagnostic
}

type TokenKind int

const (
	TokenWhitespace TokenKind = iota
	TokenComment
	TokenLParen
	TokenRParen
	TokenString
	TokenSymbol
)

type Token struct {
	Kind   TokenKind
	Raw    string
	Start  int
	End    int
	Line   int
	Column int
}

type Argument struct {
	TokenIndex int
	Kind       TokenKind
	Raw        string
}

type Expression struct {
	StartToken int
	EndToken   int
	Head       string
	Args       []Argument
}

type Edit struct {
	Start       int
	End         int
	Replacement string
}

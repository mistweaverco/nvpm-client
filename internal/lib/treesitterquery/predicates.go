package treesitterquery

func nextNonTrivia(tokens []Token, i int) int {
	for i < len(tokens) {
		switch tokens[i].Kind {
		case TokenWhitespace, TokenComment:
			i++
		default:
			return i
		}
	}
	return i
}

func matchingRParen(tokens []Token, lparenIdx int) (int, bool) {
	depth := 0
	for i := lparenIdx; i < len(tokens); i++ {
		switch tokens[i].Kind {
		case TokenLParen:
			depth++
		case TokenRParen:
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

func discoverExpressions(tokens []Token) []Expression {
	var exprs []Expression
	for i := 0; i < len(tokens); i++ {
		if tokens[i].Kind != TokenLParen {
			continue
		}
		rp, ok := matchingRParen(tokens, i)
		if !ok {
			continue
		}
		j := nextNonTrivia(tokens, i+1)
		if j >= rp || tokens[j].Kind != TokenSymbol {
			continue
		}
		head := tokens[j].Raw
		var args []Argument
		k := nextNonTrivia(tokens, j+1)
		for k < rp {
			tok := tokens[k]
			if tok.Kind == TokenLParen {
				sub, ok := matchingRParen(tokens, k)
				if !ok {
					break
				}
				k = nextNonTrivia(tokens, sub+1)
				continue
			}
			args = append(args, Argument{TokenIndex: k, Kind: tok.Kind, Raw: tok.Raw})
			k = nextNonTrivia(tokens, k+1)
		}
		exprs = append(exprs, Expression{
			StartToken: i,
			EndToken:   rp,
			Head:       head,
			Args:       args,
		})
	}
	return exprs
}

func lastStringArg(expr Expression) (Argument, bool) {
	for i := len(expr.Args) - 1; i >= 0; i-- {
		if expr.Args[i].Kind == TokenString {
			return expr.Args[i], true
		}
	}
	return Argument{}, false
}

func regexRuleName(head string) string {
	switch head {
	case "#match?":
		return RuleRegexMatch
	case "#not-match?":
		return RuleRegexNotMatch
	case "#any-match?":
		return RuleRegexAnyMatch
	case "#any-not-match?":
		return RuleRegexAnyNotMatch
	default:
		return ""
	}
}

func isRegexPredicate(head string) bool {
	return regexRuleName(head) != ""
}

func applyPredicateRules(tokens []Token, exprs []Expression) (edits []Edit, applied []AppliedPatch, diags []Diagnostic) {
	for _, expr := range exprs {
		headTok := tokens[nextNonTrivia(tokens, expr.StartToken+1)]
		line, col := loc(headTok)
		switch expr.Head {
		case "#is?":
			diags = append(diags, Diagnostic{
				Severity: SeverityWarning,
				Rule:     RulePropertyIs,
				Message:  "upstream Tree-sitter predicate #is? has no known lossless Neovim translation; preserved unchanged",
				Line:     line,
				Column:   col,
			})
		case "#is-not?":
			diags = append(diags, Diagnostic{
				Severity: SeverityWarning,
				Rule:     RulePropertyIsNot,
				Message:  "upstream Tree-sitter predicate #is-not? has no known lossless Neovim translation; preserved unchanged",
				Line:     line,
				Column:   col,
			})
		default:
			if !isRegexPredicate(expr.Head) {
				continue
			}
			arg, ok := lastStringArg(expr)
			if !ok {
				continue
			}
			decoded, err := DecodeQueryString(arg.Raw)
			if err != nil {
				tok := tokens[arg.TokenIndex]
				l, c := loc(tok)
				diags = append(diags, Diagnostic{
					Severity: SeverityWarning,
					Rule:     regexRuleName(expr.Head),
					Message:  "cannot decode regex string literal; leaving regex unchanged",
					Line:     l,
					Column:   c,
				})
				continue
			}
			tr, err := TranslateTreeSitterRegexToVimVeryMagic(decoded)
			if err != nil {
				tok := tokens[arg.TokenIndex]
				l, c := loc(tok)
				diags = append(diags, Diagnostic{
					Severity: SeverityWarning,
					Rule:     regexRuleName(expr.Head),
					Message:  err.Error(),
					Line:     l,
					Column:   c,
				})
				continue
			}
			tok := tokens[arg.TokenIndex]
			l, c := loc(tok)
			for _, d := range tr.Diagnostics {
				d.Rule = regexRuleName(expr.Head)
				if d.Line == 0 {
					d.Line = l
					d.Column = c
				}
				diags = append(diags, d)
			}
			if !tr.Changed {
				continue
			}
			encoded := EncodeQueryString(tr.Regex)
			if encoded == arg.Raw {
				continue
			}
			edits = append(edits, Edit{Start: tok.Start, End: tok.End, Replacement: encoded})
			applied = append(applied, AppliedPatch{Rule: regexRuleName(expr.Head), Line: l})
		}
	}
	return edits, applied, diags
}

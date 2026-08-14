package treesitterquery

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeEncodeQueryStringRoundTrip(t *testing.T) {
	raws := []string{
		`"foo"`,
		`"foo\\bar"`,
		`"foo\"bar"`,
		`"\\d+"`,
		`"\\\\"`,
		`"^foo\\s+bar$"`,
	}
	for _, raw := range raws {
		t.Run(raw, func(t *testing.T) {
			decoded, err := DecodeQueryString(raw)
			require.NoError(t, err)
			encoded := EncodeQueryString(decoded)
			decodedAgain, err := DecodeQueryString(encoded)
			require.NoError(t, err)
			require.Equal(t, decoded, decodedAgain)
		})
	}
}

func TestTranslateRegex_Supported(t *testing.T) {
	tests := []struct {
		src     string
		want    string
		changed bool
	}{
		{"abc", "abc", false},
		{"^foo$", "^foo$", false},
		{"foo.*", "foo.*", false},
		{"foo+", `\vfoo+`, true},
		{"foo?", `\vfoo?`, true},
		{"(foo|bar)", `\v(foo|bar)`, true},
		{"[abc]", "[abc]", false},
		{"[^abc]", "[^abc]", false},
		{"a{2}", `\va{2}`, true},
		{"a{2,}", `\va{2,}`, true},
		{"a{2,4}", `\va{2,4}`, true},
		{`\.`, `\.`, false},
		{`\*`, `\*`, false},
		{`(?:foo)`, `\v%(foo)`, true},
		{`\d+`, `\v[0-9]+`, true},
		{`\D`, `\v[^0-9]`, true},
		{`\w`, `\v[0-9A-Za-z_]`, true},
		{`\W`, `\v[^0-9A-Za-z_]`, true},
		{`\s+`, `\v\_s+`, true},
		{`\S`, `\v\_S`, true},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			got, err := TranslateTreeSitterRegexToVimVeryMagic(tt.src)
			require.NoError(t, err)
			require.Empty(t, got.Diagnostics)
			require.Equal(t, tt.changed, got.Changed)
			require.Equal(t, tt.want, got.Regex)
		})
	}
}

func TestTranslateRegex_Unsupported(t *testing.T) {
	cases := []string{
		`foo(?=bar)`,
		`foo(?!bar)`,
		`(?<=a)b`,
		`(?<!a)b`,
		`(foo)\1`,
		`(?P<name>foo)`,
		`(?i)foo`,
		`\p{L}`,
		`(?>foo)`,
		`a++`,
		`a*?`,
	}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			got, err := TranslateTreeSitterRegexToVimVeryMagic(src)
			require.NoError(t, err)
			require.False(t, got.Changed)
			require.Equal(t, src, got.Regex)
			require.NotEmpty(t, got.Diagnostics)
			require.Contains(t, got.Diagnostics[0].Message, "cannot safely translate")
		})
	}
}

func TestTranslateRegex_ExistingMagicPrefix(t *testing.T) {
	got, err := TranslateTreeSitterRegexToVimVeryMagic(`\vfoo+`)
	require.NoError(t, err)
	require.False(t, got.Changed)
	require.Equal(t, `\vfoo+`, got.Regex)
}

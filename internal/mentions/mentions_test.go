package mentions

import (
	"slices"
	"strings"
	"testing"
)

func TestExtract(t *testing.T) {
	long := strings.Repeat("a", 39)
	cases := []struct {
		name, in string
		want     []string
	}{
		{"none", "hello world", nil},
		{"empty", "", nil},
		{"start", "@bob can you look", []string{"bob"}},
		{"after space", "hi @bob", []string{"bob"}},
		{"after newline", "hi\n@bob", []string{"bob"}},
		{"after tab", "hi\t@bob", []string{"bob"}},
		{"after paren", "(@bob)", []string{"bob"}},
		{"after bracket", "[@bob]", []string{"bob"}},
		{"after dquote", `"@bob"`, []string{"bob"}},
		{"after squote", "'@bob'", []string{"bob"}},
		{"after other punct", "hi,@bob and x/@carol and -@dave", nil},
		{"email", "mail bob@example.com now", nil},
		{"email with mention", "bob@example.com and @anna", []string{"anna"}},
		{"lowercased", "@Bob @ALICE", []string{"bob", "alice"}},
		{"unique ordered", "@b @a @B @a @c", []string{"b", "a", "c"}},
		{"hyphen inside", "@bob-smith ok", []string{"bob-smith"}},
		{"trailing hyphen", "@bob- ok", []string{"bob"}},
		{"trailing hyphens", "@bob-- ok", []string{"bob"}},
		{"leading hyphen", "@-bob", nil},
		{"digits", "@1a2 @9", []string{"1a2", "9"}},
		{"ends at punct", "@bob, @anna. @carol! @dave? @eve: @fay;", []string{"bob", "anna", "carol", "dave", "eve", "fay"}},
		{"ends at dot name", "@bob.smith", []string{"bob"}},
		{"ends at underscore", "@bob_x", []string{"bob"}},
		{"bare at", "@ bob @", nil},
		{"double at", "@@bob", nil},
		{"max length", "@" + long, []string{long}},
		{"too long", "@" + long + "a", nil},
		{"unicode after", "@bobé", []string{"bob"}},
		{"unicode before", "é@bob", nil},
		{"inline code", "see `@bob` and @anna", []string{"anna"}},
		{"double backtick code", "``a ` @bob`` @anna", []string{"anna"}},
		{"unmatched backtick", "a ` @bob", []string{"bob"}},
		{"code then mention adjacent", "`x`@bob", nil},
		{"code spans lines", "`a\n@bob` @anna", []string{"anna"}},
		{"fenced", "x\n```\n@bob\n```\n@anna", []string{"anna"}},
		{"fenced lang", "```go\n@bob\n```", nil},
		{"tilde fence", "~~~\n@bob\n~~~\n@anna", []string{"anna"}},
		{"unclosed fence", "```\n@bob\n@anna", nil},
		{"long fence needs long close", "````\n```\n@bob\n````\n@anna", []string{"anna"}},
		{"three-backtick inline is not a fence", "```@bob``` @anna", []string{"anna"}},
		{"link text counts", "[@bob](https://x.y)", []string{"bob"}},
		{"link url", "[x](https://x.y/@bob) @anna", []string{"anna"}},
		{"link url at start", "[x](@bob) @anna", []string{"anna"}},
		{"link url with title", `[x](/a@b "hi @carol")`, []string{"carol"}},
		{"autolink", "<https://x.y/ @bob> <https://x.y/@carol> @anna", []string{"bob", "anna"}},
		{"bare url", "https://x.y/@bob and (@anna)", []string{"anna"}},
		{"crlf", "a\r\n@bob", []string{"bob"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Extract(c.in); !slices.Equal(got, c.want) {
				t.Errorf("Extract(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestHas(t *testing.T) {
	m := []string{"bob"}
	if !Has(m, "Bob") || !Has(m, "@bob") || Has(m, "anna") || Has(nil, "bob") {
		t.Error("Has")
	}
}

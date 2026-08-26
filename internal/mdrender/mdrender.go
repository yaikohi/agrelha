package mdrender

import (
	"bytes"
	"regexp"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

var gcdn = regexp.MustCompile(`^https://gcdn\.thunderstore\.io/`)

var (
	md = goldmark.New(
		goldmark.WithExtensions(extension.GFM),
	)

	policy = buildPolicy()
)

func buildPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()

	p.AllowElements(
		"p", "br", "hr", "blockquote", "pre", "span", "div",
		"h1", "h2", "h3", "h4", "h5", "h6",
		"ul", "ol", "li", "dl", "dt", "dd",
		"strong", "em", "b", "i", "u", "del", "ins", "sub", "sup", "kbd", "samp", "code",
		"table", "thead", "tbody", "tfoot", "tr", "th", "td",
	)
	p.AllowAttrs("align").OnElements("th", "td")
	p.AllowAttrs("class").OnElements("code", "span", "pre")

	p.AllowAttrs("href").OnElements("a")
	p.AllowStandardURLs()
	p.AllowRelativeURLs(false)
	p.RequireNoFollowOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)

	p.AllowAttrs("src").Matching(gcdn).OnElements("img")
	p.AllowAttrs("alt").OnElements("img")

	return p
}

func Render(src string) string {
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return ""
	}
	return policy.Sanitize(buf.String())
}

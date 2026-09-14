package mdrender

import (
	"bytes"
	"net/url"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var httpsImg = regexp.MustCompile(`^https://`)

var (
	md = goldmark.New(
		goldmark.WithExtensions(extension.GFM),
	)

	policy = buildPolicy()

	parseFragment = html.ParseFragment
	renderHTML    = html.Render
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

	p.AllowAttrs("src").Matching(httpsImg).OnElements("img")
	p.AllowAttrs("alt", "title").OnElements("img")

	return p
}

func Render(src string) string {
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return ""
	}
	return proxyImages(policy.Sanitize(buf.String()))
}

func proxyImages(fragment string) string {
	body := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := parseFragment(strings.NewReader(fragment), body)
	if err != nil {
		return fragment
	}
	var buf bytes.Buffer
	for _, n := range nodes {
		rewriteImg(n)
		if err := renderHTML(&buf, n); err != nil {
			return fragment
		}
	}
	return buf.String()
}

func rewriteImg(n *html.Node) {
	if n.Type == html.ElementNode && n.Data == "img" {
		for i, a := range n.Attr {
			if a.Key == "src" && strings.HasPrefix(a.Val, "https://") {
				n.Attr[i].Val = "/img?u=" + url.QueryEscape(a.Val)
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		rewriteImg(c)
	}
}

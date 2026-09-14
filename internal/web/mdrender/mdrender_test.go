package mdrender

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"golang.org/x/net/html"
)

type failMarkdown struct {
	err error
}

func (m *failMarkdown) Convert(source []byte, writer io.Writer, opts ...parser.ParseOption) error {
	return m.err
}
func (m *failMarkdown) Parser() parser.Parser        { return nil }
func (m *failMarkdown) SetParser(p parser.Parser)    {}
func (m *failMarkdown) Renderer() renderer.Renderer  { return nil }
func (m *failMarkdown) SetRenderer(r renderer.Renderer) {}

func TestRender(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
		omits    []string
	}{
		{
			name:     "basic formatting",
			input:    "# Heading 1\n\n**bold** and *italic* and ~~strike~~ and `code`",
			contains: []string{"<h1>Heading 1</h1>", "<strong>bold</strong>", "<em>italic</em>", "<del>strike</del>", "<code>code</code>"},
		},
		{
			name:     "links sanitized and target blank",
			input:    "[Google](https://google.com)",
			contains: []string{"<a ", "href=\"https://google.com\"", "nofollow", "noopener", "target=\"_blank\""},
		},
		{
			name:     "disallowed elements stripped",
			input:    "safe text\n\n<script>alert('xss')</script><iframe src=\"https://evil.com\"></iframe>",
			contains: []string{"safe text"},
			omits:    []string{"<script>", "alert", "<iframe>"},
		},
		{
			name:     "images proxied via /img",
			input:    "![Logo](https://example.com/logo.png \"Alt title\")",
			contains: []string{"<img ", "src=\"/img?u=https%3A%2F%2Fexample.com%2Flogo.png\"", "alt=\"Logo\""},
		},
		{
			name:     "tables rendered",
			input:    "| Name | Status |\n|:---|:---|\n| server | running |\n",
			contains: []string{"<table>", "<thead>", "<tbody>", "<th>Name</th>", "<td>server</td>"},
		},
		{
			name:     "empty input",
			input:    "",
			contains: []string{""},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := Render(tc.input)
			for _, exp := range tc.contains {
				if !strings.Contains(out, exp) {
					t.Errorf("expected output to contain %q, got %q", exp, out)
				}
			}
			for _, om := range tc.omits {
				if strings.Contains(out, om) {
					t.Errorf("expected output to omit %q, got %q", om, out)
				}
			}
		})
	}
}

func TestRenderErrorPaths(t *testing.T) {
	// 1. md.Convert failure
	oldMD := md
	md = &failMarkdown{err: errors.New("convert failure")}
	defer func() { md = oldMD }()

	if out := Render("test"); out != "" {
		t.Errorf("expected empty string on md.Convert error, got %q", out)
	}
	md = oldMD

	// 2. parseFragment error in proxyImages
	oldParse := parseFragment
	parseFragment = func(r io.Reader, context *html.Node) ([]*html.Node, error) {
		return nil, errors.New("parse error")
	}
	defer func() { parseFragment = oldParse }()

	if out := Render("![img](https://example.com/pic.png)"); !strings.Contains(out, "https://example.com/pic.png") {
		t.Errorf("expected original fragment on parseFragment error, got %q", out)
	}
	parseFragment = oldParse

	// 3. renderHTML error in proxyImages
	oldRender := renderHTML
	renderHTML = func(w io.Writer, n *html.Node) error {
		return errors.New("render error")
	}
	defer func() { renderHTML = oldRender }()

	if out := Render("![img](https://example.com/pic.png)"); !strings.Contains(out, "https://example.com/pic.png") {
		t.Errorf("expected original fragment on renderHTML error, got %q", out)
	}
	renderHTML = oldRender
}

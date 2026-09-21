package main

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const htmlCodeStyle = "font-family:'Courier New';background-color:#f2f2f2;"

func escapeHTML(s string) string { return string(util.EscapeHTML([]byte(s))) }

// renderHTML produces a self-contained document with inline styles. Images
// remain HTTPS references; conversion never reads or fetches external assets.
func renderHTML(markdown string) (string, error) {
	return renderHTMLWithOptions(markdown, options{})
}

func renderHTMLWithOptions(markdown string, opts options) (string, error) {
	if !utf8.ValidString(markdown) {
		return "", fmt.Errorf("Markdown input must be valid UTF-8")
	}
	if opts.RelativeLinks != "" && opts.RelativeLinks != "error" && opts.RelativeLinks != "text" {
		return "", fmt.Errorf("--relative-links must be 'error' or 'text'")
	}
	metadata, source := splitFrontmatter(normalizeSource(markdown))
	md := markdownParser(false)
	doc := md.Parser().Parse(text.NewReader(source))
	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.(type) {
		case *ast.Link, *ast.AutoLink, *ast.Image:
			_, image := n.(*ast.Image)
			url := linkURL(n, source)
			if !allowedURL(url, image) {
				if !image && opts.RelativeLinks == "text" && relativeLink(url) {
					return ast.WalkContinue, nil
				}
				kind, required := "link", "an absolute HTTP(S) or mailto URL"
				if image {
					kind, required = "image", "an absolute HTTPS URL"
				}
				return ast.WalkStop, fmt.Errorf("unsupported %s target %q (use %s)", kind, url, required)
			}
		case *ast.Paragraph:
			n.SetAttributeString("style", []byte("margin:0 0 10pt 0;"))
		case *ast.Blockquote:
			n.SetAttributeString("style", []byte("margin:0 0 10pt 18pt;padding-left:10pt;border-left:3px solid #cccccc;"))
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return "", err
	}
	md.Renderer().AddOptions(renderer.WithNodeRenderers(util.Prioritized(styledHTMLRenderer{}, 100)))
	var body bytes.Buffer
	if len(metadata) > 0 {
		body.WriteString(`<pre style="font-family:inherit;white-space:pre-wrap;break-inside:avoid;page-break-inside:avoid;">` + escapeHTML(string(metadata)) + "</pre>\n")
	}
	if err := md.Renderer().Render(&body, source, doc); err != nil {
		return "", err
	}
	return "<!doctype html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n" +
		"<title>Document</title>\n<style>h1,h2,h3,h4,h5,h6{break-after:avoid;page-break-after:avoid}tr,pre{break-inside:avoid;page-break-inside:avoid}thead{display:table-header-group}p,li{orphans:2;widows:2}</style>\n</head>\n" +
		"<body style=\"font-family:Arial,sans-serif;font-size:11pt;line-height:1.4;\">\n" +
		body.String() + "</body>\n</html>\n", nil
}

type styledHTMLRenderer struct{}

func (r styledHTMLRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	for _, kind := range []ast.NodeKind{ast.KindLink, ast.KindCodeSpan, ast.KindCodeBlock, ast.KindFencedCodeBlock, extast.KindTable, extast.KindTableCell, extast.KindStrikethrough, ast.KindText, ast.KindString} {
		reg.Register(kind, r.render)
	}
}

func (styledHTMLRenderer) render(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	write := func(s string) { _, _ = w.WriteString(s) }
	switch n := node.(type) {
	case *ast.Link:
		target := linkURL(n, source)
		if relativeLink(target) {
			if !entering {
				write(" (" + escapeHTML(target) + ")")
			}
		} else if entering {
			write(`<a href="` + escapeHTML(target) + `"`)
			if n.Title != nil {
				write(` title="` + escapeHTML(decodedText(n.Title)) + `"`)
			}
			write(">")
		} else {
			write("</a>")
		}
	case *ast.CodeSpan:
		if entering {
			write(`<code style="` + htmlCodeStyle + `">` + escapeHTML(codeSpan(n, source)))
			return ast.WalkSkipChildren, nil
		}
		write("</code>")
	case *ast.CodeBlock, *ast.FencedCodeBlock:
		if entering {
			write(`<pre style="` + htmlCodeStyle + `padding:8pt;white-space:pre-wrap;"><code style="` + htmlCodeStyle + `">`)
			write(escapeHTML(string(n.Lines().Value(source))))
		} else {
			write("</code></pre>\n")
		}
	case *extast.Table:
		if entering {
			write("<table style=\"border-collapse:collapse;margin-bottom:10pt;\">\n")
		} else {
			write("</table>\n")
		}
	case *extast.TableCell:
		header := n.Parent().Kind() == extast.KindTableHeader
		tag, cellStyle := "td", "border:1px solid #cccccc;padding:6pt;"
		if header {
			tag, cellStyle = "th", cellStyle+"background-color:#f2f2f2;"
		}
		if entering {
			if n.Alignment != extast.AlignNone {
				cellStyle = "text-align:" + n.Alignment.String() + ";" + cellStyle
			}
			write("<" + tag + ` style="` + cellStyle + `">`)
		} else {
			write("</" + tag + ">\n")
		}
	case *extast.Strikethrough:
		if entering {
			write("<s>")
		} else {
			write("</s>")
		}
	case *ast.Text:
		if entering {
			value := string(n.Value(source))
			if !n.IsRaw() {
				value = decodedText(n.Value(source))
			}
			write(escapeHTML(value))
			if n.HardLineBreak() {
				write("<br>\n")
			} else if n.SoftLineBreak() {
				write("\n")
			}
		}
	case *ast.String:
		if entering {
			value := string(n.Value)
			if !n.IsRaw() {
				value = decodedText(n.Value)
			}
			write(escapeHTML(strings.ReplaceAll(value, "\x00", "\ufffd")))
		}
	}
	return ast.WalkContinue, nil
}

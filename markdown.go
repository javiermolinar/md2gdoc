package main

import (
	"fmt"
	"maps"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type run struct {
	text  string
	style style
}

type bullet struct {
	preset string
	group  int
}

type paragraph struct {
	runs         []run
	style        style
	prefix       string
	bullet       *bullet
	keepTogether bool
}

func (p *paragraph) text() string {
	var b strings.Builder
	b.WriteString(p.prefix)
	for _, run := range p.runs {
		b.WriteString(run.text)
	}
	return b.String()
}

type block struct {
	paragraph *paragraph
	rows      [][]*paragraph
}

func markdownParser(rawHTML bool) goldmark.Markdown {
	// HTML output treats raw HTML as Markdown text, matching html:false in
	// markdown-it. Request output recognizes it so it can fail explicitly.
	blocks := parser.DefaultBlockParsers()
	inlines := parser.DefaultInlineParsers()
	for i := range inlines {
		if inlines[i].Value == parser.NewLinkParser() || inlines[i].Value == parser.NewAutoLinkParser() {
			inlines[i].Value = safeLinkParser{inlines[i].Value.(parser.InlineParser)}
		}
	}
	if !rawHTML {
		blocks = slicesWithout(blocks, parser.NewHTMLBlockParser())
		inlines = slicesWithout(inlines, parser.NewRawHTMLParser())
	}
	return goldmark.New(
		goldmark.WithParser(parser.NewParser(parser.WithBlockParsers(blocks...), parser.WithInlineParsers(inlines...), parser.WithParagraphTransformers(parser.DefaultParagraphTransformers()...))),
		goldmark.WithExtensions(extension.Table, extension.Strikethrough),
	)
}

func slicesWithout(values []util.PrioritizedValue, unwanted any) []util.PrioritizedValue {
	result := make([]util.PrioritizedValue, 0, len(values))
	for _, v := range values {
		if reflect.TypeOf(v.Value) != reflect.TypeOf(unwanted) {
			result = append(result, v)
		}
	}
	return result
}

func normalizeSource(markdown string) []byte {
	return []byte(strings.ReplaceAll(strings.ReplaceAll(markdown, "\r\n", "\n"), "\r", "\n"))
}

// A closed leading --- block is frontmatter. Preserve its literal contents;
// metadata values are not Markdown and need no YAML evaluation.
func splitFrontmatter(source []byte) (metadata, body []byte) {
	body = source
	lines := strings.Split(string(source), "\n")
	if len(lines) < 2 || lines[0] != "---" {
		return nil, body
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" || lines[i] == "..." {
			return []byte(strings.Join(lines[1:i], "\n")), []byte(strings.Join(lines[i+1:], "\n"))
		}
	}
	return nil, body
}

var entityPattern = regexp.MustCompile(`^&(?:#[xX][a-fA-F0-9]{1,6}|#[0-9]{1,7}|[a-zA-Z][a-zA-Z0-9]{1,31});`)

// Decode once, respecting escaped ampersands and Markdown's invalid numeric
// entity rules. HTML's permissive decoder alone also accepts missing semicolons.
func decodedText(value []byte) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' && i+1 < len(value) && util.IsPunct(value[i+1]) {
			i++
			b.WriteByte(value[i])
			continue
		}
		if value[i] == '&' {
			if entity := entityPattern.Find(value[i:]); entity != nil {
				if entity[1] == '#' {
					digits, base := string(entity[2:len(entity)-1]), 10
					if digits[0] == 'x' || digits[0] == 'X' {
						digits, base = digits[1:], 16
					}
					r, _ := strconv.ParseInt(digits, base, 32)
					if r <= 8 || r == 11 || r >= 14 && r <= 31 || r >= 127 && r <= 159 || r >= 0xd800 && r <= 0xdfff || r >= 0xfdd0 && r <= 0xfdef || r&0xffff >= 0xfffe || r > 0x10ffff {
						r = 0xfffd
					}
					b.WriteRune(rune(r))
				} else if decoded, ok := util.LookUpHTML5EntityByName(string(entity[1 : len(entity)-1])); ok {
					b.Write(decoded.Characters)
				} else {
					b.Write(entity)
				}
				i += len(entity) - 1
				continue
			}
		}
		b.WriteByte(value[i])
	}
	return b.String()
}

// Keep dangerous link syntax as literal text, as markdown-it does. Relative
// links remain links so both outputs can reject them or render their targets.
type safeLinkParser struct{ parser.InlineParser }

func (p safeLinkParser) Parse(parent ast.Node, reader text.Reader, context parser.Context) ast.Node {
	_, initial := reader.Position()
	n := p.InlineParser.Parse(parent, reader, context)
	if n == nil {
		return nil
	}
	url := linkURL(n, reader.Source())
	if url == "" || !gmhtml.IsDangerousURL([]byte(url)) {
		return n
	}
	start := n.Pos()
	if _, auto := n.(*ast.AutoLink); auto {
		start = initial.Start
	}
	_, end := reader.Position()
	return ast.NewString(reader.Source()[start:end.Start])
}

func (p safeLinkParser) CloseBlock(parent ast.Node, reader text.Reader, context parser.Context) {
	if closer, ok := p.InlineParser.(parser.CloseBlocker); ok {
		closer.CloseBlock(parent, reader, context)
	}
}

func linkURL(n ast.Node, source []byte) string {
	switch n := n.(type) {
	case *ast.Link:
		return string(util.URLEscape(n.Destination, true))
	case *ast.Image:
		return string(util.URLEscape(n.Destination, true))
	case *ast.AutoLink:
		url := string(util.URLEscape(n.URL(source), false))
		if n.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(url), "mailto:") {
			url = "mailto:" + url
		}
		return url
	}
	return ""
}

func allowedURL(url string, image bool) bool {
	url = strings.ToLower(url)
	return strings.HasPrefix(url, "https://") || !image && (strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "mailto:"))
}

func relativeLink(target string) bool {
	u, err := url.Parse(target)
	return err == nil && !u.IsAbs()
}

func unsupported(description string) error {
	return fmt.Errorf("unsupported Markdown: %s", description)
}

func codeSpan(n ast.Node, source []byte) string {
	var b strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		b.WriteString(strings.ReplaceAll(string(c.(*ast.Text).Value(source)), "\n", " "))
	}
	return b.String()
}

func inlineRuns(parent ast.Node, source []byte, base style, opts options) ([]run, error) {
	var runs []run
	add := func(value string, s style) {
		if value != "" {
			runs = append(runs, run{value, maps.Clone(s)})
		}
	}
	var walk func(ast.Node, style) error
	walk = func(n ast.Node, s style) error {
		childStyle := maps.Clone(s)
		var plainTarget string
		switch n := n.(type) {
		case *ast.Text:
			value := string(n.Value(source))
			if !n.IsRaw() {
				value = decodedText(n.Value(source))
			}
			add(value, s)
			if n.HardLineBreak() {
				return unsupported("explicit line breaks (use separate paragraphs)")
			}
			if n.SoftLineBreak() {
				add(" ", s)
			}
			return nil
		case *ast.String:
			value := string(n.Value)
			if !n.IsRaw() {
				value = decodedText(n.Value)
			}
			add(value, s)
			return nil
		case *ast.CodeSpan:
			maps.Copy(childStyle, codeStyle())
			add(codeSpan(n, source), childStyle)
			return nil
		case *ast.Emphasis:
			if n.Level == 2 {
				childStyle["bold"] = true
			} else {
				childStyle["italic"] = true
			}
		case *extast.Strikethrough:
			childStyle["strikethrough"] = true
		case *ast.Link, *ast.AutoLink:
			url := linkURL(n, source)
			if !allowedURL(url, false) {
				if opts.RelativeLinks == "text" && relativeLink(url) {
					plainTarget = url
				} else {
					return unsupported(fmt.Sprintf("link target %q (use an absolute HTTP(S) or mailto URL, or --relative-links text for local links)", url))
				}
			} else {
				childStyle["link"] = map[string]any{"url": url}
			}
			if auto, ok := n.(*ast.AutoLink); ok {
				add(string(auto.Label(source)), childStyle)
				return nil
			}
		case *ast.Image:
			return unsupported("images")
		case *ast.RawHTML:
			return unsupported("html_inline")
		default:
			return unsupported(n.Kind().String())
		}
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			if err := walk(c, childStyle); err != nil {
				return err
			}
		}
		if plainTarget != "" {
			add(" ("+plainTarget+")", s)
		}
		return nil
	}
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		if err := walk(n, base); err != nil {
			return nil, err
		}
	}
	return runs, nil
}

func parseRequests(markdown string, opts options) ([]block, error) {
	metadata, source := splitFrontmatter(normalizeSource(markdown))
	doc := markdownParser(true).Parser().Parse(text.NewReader(source))
	var blocks []block
	if len(metadata) > 0 {
		blocks = append(blocks, block{paragraph: &paragraph{runs: []run{{string(metadata), style{}}}, style: style{}, keepTogether: true}})
	}
	type listFrame struct {
		bullet
		pending bool
	}
	var lists []*listFrame
	quoteDepth, nextGroup := 0, 0
	emit := func(runs []run, s style) {
		p := &paragraph{runs: runs, style: s}
		if quoteDepth > 0 {
			p.style["indentStart"], p.style["indentFirstLine"] = pt(18*quoteDepth), pt(18*quoteDepth)
		}
		if len(lists) > 0 {
			frame := lists[len(lists)-1]
			if frame.pending {
				p.prefix = strings.Repeat("\t", len(lists)-1)
				b := frame.bullet
				p.bullet = &b
				frame.pending = false
			} else {
				p.style["indentStart"], p.style["indentFirstLine"] = pt(18*len(lists)), pt(18*len(lists))
			}
		}
		blocks = append(blocks, block{paragraph: p})
	}
	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			switch n.(type) {
			case *ast.Blockquote:
				quoteDepth--
			case *ast.List:
				lists = lists[:len(lists)-1]
			case *ast.ListItem:
				if lists[len(lists)-1].pending {
					emit(nil, style{})
				}
			}
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Document:
		case *ast.LinkReferenceDefinition:
			return ast.WalkSkipChildren, nil
		case *ast.Paragraph, *ast.TextBlock, *ast.Heading:
			runs, err := inlineRuns(n, source, style{}, opts)
			if err != nil {
				return ast.WalkStop, err
			}
			s := style{}
			if h, ok := n.(*ast.Heading); ok {
				s["namedStyleType"] = fmt.Sprintf("HEADING_%d", h.Level)
				s["keepWithNext"] = true
				s["keepLinesTogether"] = true
			}
			emit(runs, s)
			return ast.WalkSkipChildren, nil
		case *ast.CodeBlock, *ast.FencedCodeBlock:
			if len(lists) > 0 {
				return ast.WalkStop, unsupported("code blocks inside lists")
			}
			value := strings.TrimSuffix(string(n.Lines().Value(source)), "\n")
			emit([]run{{value, codeStyle()}}, style{})
			blocks[len(blocks)-1].paragraph.keepTogether = true
			return ast.WalkSkipChildren, nil
		case *ast.Blockquote:
			if len(lists) > 0 {
				return ast.WalkStop, unsupported("blockquotes inside lists")
			}
			quoteDepth++
		case *ast.List:
			if quoteDepth > 0 {
				return ast.WalkStop, unsupported("lists inside blockquotes")
			}
			preset := "BULLET_DISC_CIRCLE_SQUARE"
			if n.IsOrdered() {
				preset = "NUMBERED_DECIMAL_ALPHA_ROMAN"
				if n.Start != 1 {
					return ast.WalkStop, unsupported("ordered lists starting at a number other than 1")
				}
			}
			group := nextGroup
			if len(lists) > 0 {
				parent := lists[len(lists)-1]
				if parent.preset != preset {
					return ast.WalkStop, unsupported("mixed ordered/unordered list nesting")
				}
				if parent.pending {
					emit(nil, style{})
				}
				group = parent.group
			} else {
				nextGroup++
			}
			lists = append(lists, &listFrame{bullet: bullet{preset, group}})
		case *ast.ListItem:
			lists[len(lists)-1].pending = true
		case *extast.Table:
			if len(lists) > 0 {
				return ast.WalkStop, unsupported("tables inside lists")
			}
			if quoteDepth > 0 {
				return ast.WalkStop, unsupported("tables inside blockquotes")
			}
			table := block{}
			for row := n.FirstChild(); row != nil; row = row.NextSibling() {
				var cells []*paragraph
				for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
					base := style{}
					if row.Kind() == extast.KindTableHeader {
						base["bold"] = true
					}
					runs, err := inlineRuns(cell, source, base, opts)
					if err != nil {
						return ast.WalkStop, err
					}
					alignment := "START"
					switch cell.(*extast.TableCell).Alignment {
					case extast.AlignCenter:
						alignment = "CENTER"
					case extast.AlignRight:
						alignment = "END"
					}
					cells = append(cells, &paragraph{runs: runs, style: style{"alignment": alignment}})
				}
				table.rows = append(table.rows, cells)
			}
			blocks = append(blocks, table)
			return ast.WalkSkipChildren, nil
		case *ast.ThematicBreak:
			return ast.WalkStop, unsupported("thematic breaks")
		case *ast.HTMLBlock:
			return ast.WalkStop, unsupported("html_block")
		default:
			return ast.WalkStop, unsupported(n.Kind().String())
		}
		return ast.WalkContinue, nil
	})
	return blocks, err
}

package main

import (
	"strings"
	"testing"
)

func TestHTML(t *testing.T) {
	cases := []struct {
		name, markdown     string
		contains, excludes []string
	}{
		{"formatting", "# Title 😀\n\n**Bold**, *italic*, ~~gone~~ and [link](https://example.com).", []string{"<!doctype html>\n<html>", `<meta charset="utf-8">`, "<h1>Title 😀</h1>", "<strong>Bold</strong>", "<em>italic</em>", "<s>gone</s>", `<a href="https://example.com">link</a>`}, nil},
		{"tables", "| Left | Right |\n| :--- | ---: |\n| **A** | 42 |", []string{`<table style="border-collapse:collapse;`, `<th style="text-align:left;border:`, `<td style="text-align:right;border:`, `<strong>A</strong>`}, nil},
		{"lists quotes breaks", "3. Third\n   - Child\n\n> Quote\n\nOne  \nTwo\n\n---", []string{`<ol start="3">`, "<ul>\n<li>Child</li>", "<blockquote style=", "One<br>\nTwo", "<hr>"}, nil},
		{"code", "`<tag>`\n\n```html\n<script>alert(1)</script>\n& more\n```", []string{"<code style=", "&lt;tag&gt;</code>", "&lt;script&gt;alert(1)&lt;/script&gt;\n&amp; more\n</code></pre>", "font-family:'Courier New'"}, []string{"<script>", "language-html"}},
		{"indented code", "    code\n", []string{"<pre style="}, nil},
		{"raw HTML", "<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>\n\n<!-- comment -->", []string{"&lt;script&gt;", "&lt;img", "&lt;!-- comment --&gt;"}, []string{"<script", "<img", "<!--"}},
		{"images", "![A & B](https://example.com/image.png \"Caption\")", []string{`<img src="https://example.com/image.png" alt="A &amp; B" title="Caption">`}, nil},
		{"unsafe link", "[click](javascript:alert(1))", []string{"[click](javascript:alert(1))"}, []string{"<a "}},
		{"encoded unsafe link", "[click](jav&#x61;script:alert(1))", []string{"[click](javascript:alert(1))"}, []string{"<a "}},
		{"data link", "[click](data:text/html;base64,AAAA)", nil, []string{"<a "}},
		{"unsafe autolink", "Before <javascript:alert(1)> after", []string{"Before &lt;javascript:alert(1)&gt; after"}, []string{"<a "}},
		{"file image", "![x](file:///etc/passwd)", []string{"![x](file:///etc/passwd)"}, []string{"<img "}},
		{"attribute injection", "[x](https://example.com \"\\\" onmouseover=\\\"alert(1)\")", nil, []string{` onmouseover="`}},
		{"empty", "", []string{"</body>\n</html>\n"}, nil},
		{"reference links", "[Example][ref]\n\n[ref]: https://example.com", []string{`<a href="https://example.com">Example</a>`}, []string{"[ref]:"}},
		{"link titles", `[Example](https://example.com "A &amp; B &copycat;")`, []string{`title="A &amp; B &amp;copycat;"`}, nil},
		{"print pagination", "# Heading\n\n```\ncode\n```\n\n| H |\n|---|\n| C |", []string{"break-after:avoid", "tr,pre{break-inside:avoid", "thead{display:table-header-group}", "orphans:2;widows:2"}, nil},
		{"table in quote", "> | H |\n> |---|\n> | B |", []string{"<blockquote style=", "<table style="}, nil},
		{"table in list", "- | H |\n  |---|\n  | B |", []string{"<ul>", "<table style="}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := renderHTML(tc.markdown)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in %s", want, got)
				}
			}
			for _, unwanted := range tc.excludes {
				if strings.Contains(got, unwanted) {
					t.Errorf("unexpected %q in %s", unwanted, got)
				}
			}
			if !strings.HasSuffix(got, "</body>\n</html>\n") {
				t.Error("missing document ending")
			}
			again, _ := renderHTML(tc.markdown)
			if got != again {
				t.Error("HTML is not deterministic")
			}
		})
	}
}

func TestHTMLRejectsUnsupportedURLs(t *testing.T) {
	for _, markdown := range []string{"[local](./notes.md)", "[anchor](#here)", "![local](./secret.png)", "![insecure](http://example.com/image.png)", "![inline](data:image/png;base64,AAAA)"} {
		t.Run(markdown, func(t *testing.T) {
			if _, err := renderHTML(markdown); err == nil || !strings.Contains(err.Error(), "target") {
				t.Fatalf("expected URL error, got %v", err)
			}
		})
	}
}

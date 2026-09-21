package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodedText(t *testing.T) {
	for input, want := range map[string]string{
		`&amp;`: "&", `\&amp;`: "&amp;", `&amp;amp;`: "&amp;", `&copy`: "&copy", `&#x1F680;`: "🚀",
		`&#x1B;`: "\ufffd", `&#xFFFF;`: "\ufffd", `&#0;`: "\ufffd", `&#13;`: "\r", `&#09;`: "\t", `&#xE000;`: "\ue000",
		`&copycat;`: "&copycat;", `&notarealentity;`: "&notarealentity;", `&notin;`: "∉", `&NotEqualTilde;`: "≂̸",
	} {
		t.Run(input, func(t *testing.T) {
			if got := decodedText([]byte(input)); got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

func TestUnknownEntitiesRemainLiteralInBothOutputs(t *testing.T) {
	input := "Keep &notarealentity; and &copycat; literally."
	body, err := convert(input, options{})
	if err != nil {
		t.Fatal(err)
	}
	if body.Requests[0].InsertText.Text != input+"\n" {
		t.Fatalf("changed text: %q", body.Requests[0].InsertText.Text)
	}
	html, err := renderHTML(input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "Keep &amp;notarealentity; and &amp;copycat; literally.") {
		t.Fatalf("changed HTML: %s", html)
	}
}

func TestFrontmatterPreservedLiterally(t *testing.T) {
	for _, ending := range []string{"---", "..."} {
		input := "---\nAuthor: **Javi**\nCreated: 2026-09-04\nNested:\n  value: &copycat;\n" + ending + "\n\n# Title"
		body, err := convert(input, options{})
		if err != nil {
			t.Fatal(err)
		}
		want := "Author: **Javi**\nCreated: 2026-09-04\nNested:\n  value: &copycat;\nTitle\n"
		if body.Requests[0].InsertText.Text != want {
			t.Fatalf("metadata changed: %q", body.Requests[0].InsertText.Text)
		}
		html, err := renderHTML(strings.ReplaceAll(input, "\n", "\r\n"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(html, "Author: **Javi**\nCreated: 2026-09-04\nNested:\n  value: &amp;copycat;</pre>") || !strings.Contains(html, "<h1>Title</h1>") {
			t.Fatalf("metadata changed: %s", html)
		}
	}
	for _, input := range []string{"---\nAuthor: Javi", "Text\n\n---\n\nMore"} {
		if _, err := convert(input, options{}); err == nil {
			t.Fatalf("non-frontmatter break accepted: %q", input)
		}
	}
}

func TestRelativeLinksText(t *testing.T) {
	for _, target := range []string{"./notes.md", "../notes%20two.md", "#heading", "/root", "//example.com/path"} {
		input := "[**Notes**](" + target + ")"
		body, err := convert(input, options{RelativeLinks: "text"})
		if err != nil {
			t.Fatal(err)
		}
		if body.Requests[0].InsertText.Text != "Notes ("+target+")\n" {
			t.Fatal("link label or target lost")
		}
		for _, req := range body.Requests {
			if req.UpdateTextStyle != nil && req.UpdateTextStyle.TextStyle["link"] != nil {
				t.Fatal("local hyperlink emitted")
			}
		}
		html, err := renderHTMLWithOptions(input, options{RelativeLinks: "text"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(html, "<strong>Notes</strong> ("+target+")") || strings.Contains(html, "<a ") {
			t.Fatalf("wrong fallback: %s", html)
		}
	}
	for _, input := range []string{"[x](ftp://example.com)", "![x](./image.png)"} {
		if _, err := convert(input, options{RelativeLinks: "text"}); err == nil {
			t.Fatalf("unexpected request acceptance: %s", input)
		}
		if _, err := renderHTMLWithOptions(input, options{RelativeLinks: "text"}); err == nil {
			t.Fatalf("unexpected HTML acceptance: %s", input)
		}
	}
}

func TestRequestDangerousURLsStayLiteral(t *testing.T) {
	for _, markdown := range []string{"[x](javascript:alert(1))", "![x](file:///etc/passwd)", "[x][ref]\n\n[ref]: javascript:alert(1)", "Before <javascript:alert(1)> after"} {
		body, err := convert(markdown, options{})
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(body)
		if strings.Contains(string(data), `"link":`) {
			t.Fatalf("unsafe link emitted: %s", data)
		}
	}
}

func TestInvalidUTF8(t *testing.T) {
	if _, err := convert(string([]byte{0xff}), options{}); err == nil {
		t.Error("requests accepted invalid UTF-8")
	}
	if _, err := renderHTML(string([]byte{0xff})); err == nil {
		t.Error("HTML accepted invalid UTF-8")
	}
}

func FuzzConversion(f *testing.F) {
	for _, seed := range []string{"", "# Heading\n\n**bold**", "| H |\n|---|\n| 😀 |", "- Parent\n  - Child", "![x](file:///x)", "[x][ref]\n\n[ref]: javascript:alert(1)", "&#x1B;", "[link](https://example.com)"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, markdown string) {
		if len(markdown) > 10000 {
			t.Skip()
		}
		body, err := convert(markdown, options{})
		if err == nil {
			if _, err := json.Marshal(body); err != nil {
				t.Fatal(err)
			}
		}
		_, _ = renderHTML(markdown)
	})
}

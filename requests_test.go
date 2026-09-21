package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// This corpus was captured from the TypeScript converter before the port,
// including its live-verified table request sequence. Field-mask order is not
// significant to Docs. Pagination additions are tested separately; all other
// JSON values and request order must still match the original converter.
func TestRequestCompatibility(t *testing.T) {
	data, err := os.ReadFile("testdata/requests.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Markdown string          `json:"markdown"`
		Options  options         `json:"options"`
		Body     json.RawMessage `json:"body"`
		Error    string          `json:"error"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for i, tc := range cases {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			body, err := convert(tc.Markdown, tc.Options)
			if tc.Error != "" {
				if err == nil {
					t.Fatalf("expected error for %q", tc.Markdown)
				}
				return
			}
			if err != nil {
				t.Fatalf("%q: %v", tc.Markdown, err)
			}
			got, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(normalizeJSON(t, withoutPagination(t, got)), normalizeJSON(t, tc.Body)) {
				t.Errorf("Markdown: %q\n got: %s\nwant: %s", tc.Markdown, got, tc.Body)
			}
		})
	}
}

// Keep the pre-port corpus as an independent oracle for text, table geometry,
// ranges, and formatting rather than regenerating it from the new code.
func withoutPagination(t *testing.T, data []byte) []byte {
	t.Helper()
	var body struct {
		Requests []map[string]json.RawMessage `json:"requests"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	kept := body.Requests[:0]
	for _, req := range body.Requests {
		if req["updateTableRowStyle"] != nil || req["pinTableHeaderRows"] != nil {
			continue
		}
		if raw := req["updateParagraphStyle"]; raw != nil {
			var p updateParagraphStyle
			if err := json.Unmarshal(raw, &p); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"keepWithNext", "keepLinesTogether", "avoidWidowAndOrphan", "pageBreakBefore"} {
				delete(p.ParagraphStyle, key)
			}
			p.Fields = fields(p.ParagraphStyle)
			if p.Fields == "" {
				continue
			}
			req["updateParagraphStyle"], _ = json.Marshal(p)
		}
		kept = append(kept, req)
	}
	body.Requests = kept
	result, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPagination(t *testing.T) {
	start, tab := int64(37), "t.other"
	body, err := convert("# Heading\n\n```\n😀 first\nlast\n```\n\nAfter\n\n| H |\n|---|\n| Cell |\n\n- Item", options{StartIndex: &start, TabID: &tab})
	if err != nil {
		t.Fatal(err)
	}
	var tableStart int64
	var rowStyle, pinned, codeChain, codeLines, heading, baseline bool
	for _, req := range body.Requests {
		if req.InsertTable != nil {
			tableStart = req.InsertTable.Location.Index + 1
		}
		if r := req.UpdateTableRowStyle; r != nil {
			rowStyle = r.TableStartLocation == (location{tableStart, tab}) && r.TableRowStyle["preventOverflow"] == true
		}
		if r := req.PinTableHeaderRows; r != nil {
			pinned = r.TableStartLocation == (location{tableStart, tab}) && r.PinnedHeaderRowsCount == 1
		}
		if p := req.UpdateParagraphStyle; p != nil {
			if p.Range.TabID != tab {
				t.Fatal("pagination targeted the wrong tab")
			}
			if p.Range.StartIndex == 45 && p.Range.EndIndex == 54 && p.ParagraphStyle["keepWithNext"] == true {
				codeChain = true
			}
			if p.Range.StartIndex == 45 && p.Range.EndIndex == 59 && p.ParagraphStyle["keepLinesTogether"] == true {
				codeLines = true
			}
			if p.ParagraphStyle["namedStyleType"] == "HEADING_1" {
				heading = p.ParagraphStyle["keepWithNext"] == true
			}
			if p.ParagraphStyle["namedStyleType"] == "NORMAL_TEXT" {
				baseline = p.ParagraphStyle["keepWithNext"] == false && p.ParagraphStyle["avoidWidowAndOrphan"] == true
				if strings.Contains(p.Fields, "pageBreakBefore") {
					t.Fatal("Docs rejects pageBreakBefore over a range containing tables")
				}
			}
		}
	}
	if !rowStyle || !pinned || !codeChain || !codeLines || !heading || !baseline {
		t.Fatalf("row=%t pin=%t codeChain=%t codeLines=%t heading=%t baseline=%t", rowStyle, pinned, codeChain, codeLines, heading, baseline)
	}
	if body.Requests[len(body.Requests)-1].CreateParagraphBullets == nil {
		t.Fatal("pagination must run before list creation shifts indices")
	}
}

func normalizeJSON(t *testing.T, data []byte) any {
	t.Helper()
	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				if key == "fields" {
					parts := strings.Split(child.(string), ",")
					slices.Sort(parts)
					v[key] = strings.Join(parts, ",")
				} else {
					walk(child)
				}
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(result)
	return result
}

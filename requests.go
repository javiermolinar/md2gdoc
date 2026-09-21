package main

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

const maxIndex int64 = 1<<31 - 1

type options struct {
	StartIndex    *int64  `json:"startIndex,omitempty"`
	TabID         *string `json:"tabId,omitempty"`
	RelativeLinks string  `json:"relativeLinks,omitempty"`
}

type location struct {
	Index int64  `json:"index"`
	TabID string `json:"tabId,omitempty"`
}

type docsRange struct {
	StartIndex int64  `json:"startIndex"`
	EndIndex   int64  `json:"endIndex"`
	TabID      string `json:"tabId,omitempty"`
}

type style map[string]any

type insertText struct {
	Location location `json:"location"`
	Text     string   `json:"text"`
}

type insertTable struct {
	Location location `json:"location"`
	Rows     int      `json:"rows"`
	Columns  int      `json:"columns"`
}

type updateTableRowStyle struct {
	TableStartLocation location `json:"tableStartLocation"`
	TableRowStyle      style    `json:"tableRowStyle"`
	Fields             string   `json:"fields"`
}

type pinTableHeaderRows struct {
	TableStartLocation    location `json:"tableStartLocation"`
	PinnedHeaderRowsCount int      `json:"pinnedHeaderRowsCount"`
}

type updateTextStyle struct {
	Range     docsRange `json:"range"`
	TextStyle style     `json:"textStyle"`
	Fields    string    `json:"fields"`
}

type updateParagraphStyle struct {
	Range          docsRange `json:"range"`
	ParagraphStyle style     `json:"paragraphStyle"`
	Fields         string    `json:"fields"`
}

type deleteParagraphBullets struct {
	Range docsRange `json:"range"`
}

type createParagraphBullets struct {
	Range        docsRange `json:"range"`
	BulletPreset string    `json:"bulletPreset"`
}

// request is the subset of documents.batchUpdate emitted by this converter.
// Every location and range uses UTF-16 code units, including table structure.
type request struct {
	InsertText             *insertText             `json:"insertText,omitempty"`
	InsertTable            *insertTable            `json:"insertTable,omitempty"`
	UpdateTableRowStyle    *updateTableRowStyle    `json:"updateTableRowStyle,omitempty"`
	PinTableHeaderRows     *pinTableHeaderRows     `json:"pinTableHeaderRows,omitempty"`
	UpdateTextStyle        *updateTextStyle        `json:"updateTextStyle,omitempty"`
	UpdateParagraphStyle   *updateParagraphStyle   `json:"updateParagraphStyle,omitempty"`
	DeleteParagraphBullets *deleteParagraphBullets `json:"deleteParagraphBullets,omitempty"`
	CreateParagraphBullets *createParagraphBullets `json:"createParagraphBullets,omitempty"`
}

type batchUpdateBody struct {
	Requests []request `json:"requests"`
}

func utf16Len(text string) int64 {
	var length int64
	for _, r := range text {
		length++
		if r > 0xffff {
			length++
		}
	}
	return length
}

func fields(s style) string {
	keys := make([]string, 0, len(s))
	for key := range s {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return strings.Join(keys, ",")
}

func pt(n int) any {
	return map[string]any{"magnitude": n, "unit": "PT"}
}

func codeStyle() style {
	return style{
		"weightedFontFamily": map[string]any{"fontFamily": "Courier New"},
		"backgroundColor": map[string]any{"color": map[string]any{"rgbColor": map[string]any{
			"red": 0.95, "green": 0.95, "blue": 0.95,
		}}},
	}
}

func strippedCharacters(text string, rendered bool) bool {
	for _, r := range text {
		if r <= 8 || r == 12 || r >= 14 && r <= 31 || r >= 0xe000 && r <= 0xf8ff || rendered && r == 13 {
			return true
		}
	}
	return false
}

// convert is pure: callers own authentication, paragraph-boundary selection,
// deletion of replaced content, revision guards, and the single API call.
func convert(markdown string, opts options) (batchUpdateBody, error) {
	body := batchUpdateBody{Requests: []request{}}
	start := int64(1)
	if opts.StartIndex != nil {
		start = *opts.StartIndex
	}
	if start < 1 || start > maxIndex {
		return body, fmt.Errorf("startIndex must be an integer between 1 and 2147483647")
	}
	tabID := ""
	if opts.TabID != nil {
		tabID = *opts.TabID
		if strings.TrimSpace(tabID) == "" {
			return body, fmt.Errorf("tabId must be a non-empty string when provided")
		}
	}
	if !utf8.ValidString(markdown) {
		return body, fmt.Errorf("Markdown input must be valid UTF-8")
	}
	if strippedCharacters(markdown, false) {
		return body, fmt.Errorf("input contains control or private-use characters removed by Google Docs")
	}
	if opts.RelativeLinks != "" && opts.RelativeLinks != "error" && opts.RelativeLinks != "text" {
		return body, fmt.Errorf("--relative-links must be 'error' or 'text'")
	}
	blocks, err := parseRequests(markdown, opts)
	if err != nil || len(blocks) == 0 {
		return body, err
	}
	rangeAt := func(from, to int64) docsRange { return docsRange{from, to, tabID} }
	textRequest := func(index int64, text string) request {
		return request{InsertText: &insertText{location{index, tabID}, text}}
	}
	type positionedParagraph struct {
		paragraph *paragraph
		start     int64
	}
	var paragraphs []positionedParagraph
	cursor := start
	var pending strings.Builder
	pendingStart := start
	flush := func() {
		if pending.Len() > 0 {
			body.Requests = append(body.Requests, textRequest(pendingStart, pending.String()))
			pending.Reset()
		}
	}
	for _, b := range blocks {
		if b.paragraph != nil {
			p := b.paragraph
			if pending.Len() == 0 {
				pendingStart = cursor
			}
			paragraphs = append(paragraphs, positionedParagraph{p, cursor})
			text := p.text() + "\n"
			pending.WriteString(text)
			cursor += utf16Len(text)
		} else {
			flush()
			tableLocation := cursor
			columns := len(b.rows[0])
			body.Requests = append(body.Requests, request{InsertTable: &insertTable{location{cursor, tabID}, len(b.rows), columns}})
			// Docs adds a preceding newline, table start/end markers, one row
			// marker per row, and a cell marker + paragraph newline per cell.
			cursor += 2
			var cells []request
			for r, row := range b.rows {
				cursor++
				for c, cell := range row {
					cursor++
					paragraphs = append(paragraphs, positionedParagraph{cell, cursor})
					text := cell.text()
					if text != "" {
						cells = append(cells, textRequest(tableLocation+4+int64(r*(2*columns+1)+2*c), text))
					}
					cursor += utf16Len(text) + 1 // Reuse the cell's final newline.
				}
			}
			cursor++
			// Reverse insertion keeps the still-empty cell indices valid.
			for i := len(cells) - 1; i >= 0; i-- {
				body.Requests = append(body.Requests, cells[i])
			}
			body.Requests = append(body.Requests,
				request{UpdateTableRowStyle: &updateTableRowStyle{location{tableLocation + 1, tabID}, style{"preventOverflow": true}, "preventOverflow"}},
				request{PinTableHeaderRows: &pinTableHeaderRows{location{tableLocation + 1, tabID}, 1}},
			)
		}
		if cursor > maxIndex {
			return batchUpdateBody{}, fmt.Errorf("rendered content exceeds Google Docs' index range")
		}
	}
	flush()
	for _, p := range paragraphs {
		if strippedCharacters(p.paragraph.text(), true) {
			return batchUpdateBody{}, fmt.Errorf("rendered text contains characters removed by Google Docs")
		}
	}
	all := rangeAt(start, cursor)
	body.Requests = append(body.Requests,
		request{DeleteParagraphBullets: &deleteParagraphBullets{all}},
		request{UpdateParagraphStyle: &updateParagraphStyle{all, style{
			"namedStyleType": "NORMAL_TEXT", "indentStart": pt(0), "indentEnd": pt(0), "indentFirstLine": pt(0),
			"keepWithNext": false, "keepLinesTogether": false, "avoidWidowAndOrphan": true,
		}, "namedStyleType,indentStart,indentEnd,indentFirstLine,keepWithNext,keepLinesTogether,avoidWidowAndOrphan"}},
		request{UpdateTextStyle: &updateTextStyle{all, style{
			"bold": false, "italic": false, "strikethrough": false, "underline": false,
		}, "bold,italic,strikethrough,underline,link,backgroundColor,weightedFontFamily"}},
	)
	type bulletRange struct {
		createParagraphBullets
		group int
	}
	var bullets []bulletRange
	for _, positioned := range paragraphs {
		p := positioned.paragraph
		cursor = positioned.start + utf16Len(p.prefix)
		for _, run := range p.runs {
			end := cursor + utf16Len(run.text)
			if len(run.style) > 0 && cursor < end {
				body.Requests = append(body.Requests, request{UpdateTextStyle: &updateTextStyle{rangeAt(cursor, end), run.style, fields(run.style)}})
			}
			cursor = end
		}
		cursor++
		if len(p.style) > 0 {
			body.Requests = append(body.Requests, request{UpdateParagraphStyle: &updateParagraphStyle{rangeAt(positioned.start, cursor), p.style, fields(p.style)}})
		}
		if p.keepTogether {
			body.Requests = append(body.Requests, request{UpdateParagraphStyle: &updateParagraphStyle{rangeAt(positioned.start, cursor), style{"keepLinesTogether": true}, "keepLinesTogether"}})
			// Each newline in a code block is a Docs paragraph. Link all but
			// the final line, so the following prose can still start a new page.
			if last := strings.LastIndex(p.text(), "\n"); last >= 0 {
				end := positioned.start + utf16Len(p.text()[:last+1])
				body.Requests = append(body.Requests, request{UpdateParagraphStyle: &updateParagraphStyle{rangeAt(positioned.start, end), style{"keepWithNext": true}, "keepWithNext"}})
			}
		}
		if p.bullet != nil {
			if len(bullets) > 0 && bullets[len(bullets)-1].group == p.bullet.group && bullets[len(bullets)-1].Range.EndIndex == positioned.start {
				bullets[len(bullets)-1].Range.EndIndex = cursor
			} else {
				bullets = append(bullets, bulletRange{createParagraphBullets{rangeAt(positioned.start, cursor), p.bullet.preset}, p.bullet.group})
			}
		}
	}
	// List creation removes nesting tabs. Apply it last, in reverse order.
	for i := len(bullets) - 1; i >= 0; i-- {
		body.Requests = append(body.Requests, request{CreateParagraphBullets: &bullets[i].createParagraphBullets})
	}
	return body, nil
}

// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package rtf

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-richdoc/richdoc"
)

// mustParse parses a body wrapped in the RTF header and fails the test on error.
func mustParse(t *testing.T, body string) *richdoc.Document {
	t.Helper()
	d, err := Parse([]byte(`{\rtf1\ansi `+body+`}`), Limits{})
	if err != nil {
		t.Fatalf("Parse(%q): %v", body, err)
	}
	return d
}

// dump renders a document as a compact string so a test can state the whole
// tree it expects, not a few fields of it.
func dump(d *richdoc.Document) string {
	var b strings.Builder
	for _, bl := range d.Blocks {
		dumpBlock(&b, bl)
	}
	return b.String()
}

func dumpBlock(b *strings.Builder, bl richdoc.Block) {
	switch v := bl.(type) {
	case richdoc.Paragraph:
		fmt.Fprintf(b, "P%s[%s]", classSuffix(v.Classes), dumpInl(v.Inlines))
	case richdoc.Heading:
		fmt.Fprintf(b, "H%d[%s]", v.Level, dumpInl(v.Inlines))
	case richdoc.ThematicBreak:
		b.WriteString("HR")
	case richdoc.List:
		o := "UL"
		if v.Ordered {
			o = fmt.Sprintf("OL%d", v.Start)
		}
		b.WriteString(o + "{")
		for i, it := range v.Items {
			if i > 0 {
				b.WriteString(",")
			}
			for _, x := range it.Blocks {
				dumpBlock(b, x)
			}
		}
		b.WriteString("}")
	case richdoc.Table:
		b.WriteString("T{")
		if len(v.Header) > 0 {
			b.WriteString("h:" + dumpCells(v.Header) + ";")
		}
		for _, row := range v.Rows {
			b.WriteString(dumpCells(row) + ";")
		}
		b.WriteString("}")
	default:
		fmt.Fprintf(b, "?%T", bl)
	}
}

func classSuffix(c []string) string {
	if len(c) == 0 {
		return ""
	}
	return "." + strings.Join(c, ".")
}

func dumpCells(cs []richdoc.Cell) string {
	var p []string
	for _, c := range cs {
		s := dumpInl(c.Inlines)
		if c.ColSpan > 1 {
			s += fmt.Sprintf("/c%d", c.ColSpan)
		}
		if c.RowSpan > 1 {
			s += fmt.Sprintf("/r%d", c.RowSpan)
		}
		p = append(p, s)
	}
	return strings.Join(p, "|")
}

func dumpInl(in []richdoc.Inline) string {
	var b strings.Builder
	for _, x := range in {
		switch v := x.(type) {
		case richdoc.Text:
			b.WriteString(v.Value)
		case richdoc.Strong:
			b.WriteString("<b>" + dumpInl(v.Inlines) + "</b>")
		case richdoc.Emph:
			b.WriteString("<i>" + dumpInl(v.Inlines) + "</i>")
		case richdoc.Strikethrough:
			b.WriteString("<s>" + dumpInl(v.Inlines) + "</s>")
		case richdoc.Link:
			fmt.Fprintf(&b, "<a %s %q>%s</a>", v.URL, v.Title, dumpInl(v.Inlines))
		case richdoc.LineBreak:
			b.WriteString("<br>")
		case richdoc.Anchor:
			b.WriteString("<#" + v.ID + ">")
		case richdoc.Footnote:
			b.WriteString("<fn:")
			for _, x := range v.Blocks {
				dumpBlock(&b, x)
			}
			b.WriteString(">")
		case richdoc.Image:
			fmt.Fprintf(&b, "<img %s %s %d>", v.Width, v.Height, v.Scale)
		default:
			fmt.Fprintf(&b, "?%T", x)
		}
	}
	return b.String()
}

// imageURL is the URL of the first image in the first paragraph.
func imageURL(t *testing.T, d *richdoc.Document) string {
	t.Helper()
	p, ok := d.Blocks[0].(richdoc.Paragraph)
	if !ok {
		t.Fatalf("first block is %T", d.Blocks[0])
	}
	for _, in := range p.Inlines {
		if img, ok := in.(richdoc.Image); ok {
			return img.URL
		}
	}
	t.Fatal("no image")
	return ""
}

// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package rtf

import (
	"reflect"
	"testing"

	"github.com/go-richdoc/richdoc"
)

// Each case sits on the edge of a guard: the value just inside it and the value
// just outside must come out differently, and the test says how.
func TestEdges(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"a cell of one heading keeps its text", `\trowd\cellx1\intbl\outlinelvl0 H\cell\row`, `T{H;}`},
		{"spaces alone are not a paragraph", `a\par   \par b\par`, `P[a]P[b]`},
		{"a stray close brace after the end", `a}}`, `P[a]`},
		{"a style sheet without a semicolon", `{\stylesheet{\s1 Heading 1}}a`, `P[a]`},
		{"a lone low surrogate", `\uc0\u-8704 x`, `P[` + "�" + `x]`},
		{"a surrogate pair", `\uc0\u-10179\u-8704 x`, `P[` + "\U0001F600" + `x]`},
		{"an empty bin", `{\pict\pngblip\bin0 }x`, `P[x]`},
		{"an empty instruction", `{\field{\*\fldinst }{\fldrslt t}}`, `P[t]`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := dump(mustParse(t, c.in)); got != c.want {
				t.Errorf("got %s, want %s", got, c.want)
			}
		})
	}
}

// Input that stops in the middle of a token must still give a document or an
// error, never a panic and never both.
func TestTruncatedTokens(t *testing.T) {
	for _, in := range []string{
		`{\rtf1 a\`, `{\rtf1 \b`, `{\rtf1 \b12`, `{\rtf1 \b-`, `{\rtf1 \'4`, `{\rtf1 \'`,
		`{\rtf1 \bin`, `{\rtf1 \bin-1 x}`, `{\rtf1{\pict\pngblip\bin5 abc}}`,
	} {
		d, err := Parse([]byte(in), Limits{})
		if (d == nil) == (err == nil) {
			t.Errorf("%q: document %v and error %v", in, d, err)
		}
	}
}

func TestFieldTokens(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
	}{
		{``, nil},
		{`a "" b`, []string{"a", "", "b"}},
		{`HYPERLINK "http://a`, []string{"HYPERLINK", "http://a"}},
		{`HYPERLINK "http://a b" \o "t"`, []string{"HYPERLINK", "http://a b", `\o`, "t"}},
	} {
		if got := fieldTokens(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("fieldTokens(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if _, _, ok := hyperlink(""); ok {
		t.Error("an empty instruction is not a hyperlink")
	}
}

// A space is allowed in a target (browsers encode it); a control character is
// not. 0x20 and 0x1f are the two sides of the edge.
func TestSafeURLControlEdge(t *testing.T) {
	if !safeURL("http://a/b c") {
		t.Error("a space is refused")
	}
	if safeURL("http://a/b" + string(rune(0x1f)) + "c") {
		t.Error("a control character is accepted")
	}
}

// A picture of exactly MaxPict bytes is read; one byte more is refused.
func TestPictLimitEdge(t *testing.T) {
	lim := Limits{MaxPict: 2}
	if _, err := Parse([]byte(`{\rtf1{\pict\pngblip 89504e}}`), lim); err == nil {
		t.Error("three bytes against a limit of two: want an error")
	}
	if _, err := Parse([]byte(`{\rtf1{\pict\pngblip 8950}}`), lim); err != nil {
		t.Errorf("two bytes against a limit of two: %v", err)
	}
}

// A byte of 0x80 is code-page text, not ASCII: in the default code page it is
// the euro sign.
func TestHighByteEdge(t *testing.T) {
	got := dump(mustParse(t, "{\\rtf1\\ansi a\x80b}"))
	if want := "P[a" + "€" + "b]"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// An empty style name, a ';' first in the style sheet text, is not an error.
func TestEmptyStyleName(t *testing.T) {
	if got := dump(mustParse(t, `{\rtf1{\stylesheet{\s1 ;}{\s2 heading 2;}}\pard\s2 A\par}`)); got != `H2[A]` {
		t.Errorf("got %s", got)
	}
}

// \bin hands a whole chunk to add, whose check is a different line from the
// per-byte one of hex data.
func TestBinLimitEdge(t *testing.T) {
	lim := Limits{MaxPict: 2}
	if _, err := Parse([]byte(`{\rtf1{\pict\pngblip\bin2 ab}}`), lim); err != nil {
		t.Errorf("two bytes against a limit of two: %v", err)
	}
	if _, err := Parse([]byte(`{\rtf1{\pict\pngblip\bin3 abc}}`), lim); err == nil {
		t.Error("three bytes against a limit of two: want an error")
	}
}

func TestMakeCellShapes(t *testing.T) {
	para := richdoc.Paragraph{Inlines: []richdoc.Inline{richdoc.Txt("a")}}
	if c := makeCell([]richdoc.Block{para}); c.Blocks != nil || len(c.Inlines) != 1 {
		t.Errorf("one paragraph: want Inlines alone, got %+v", c)
	}
	h := richdoc.Heading{Level: 1, Inlines: para.Inlines}
	if c := makeCell([]richdoc.Block{h}); len(c.Blocks) != 1 || len(c.Inlines) != 1 {
		t.Errorf("one heading: want Blocks and Inlines, got %+v", c)
	}
}

func TestHyperlinkEmptyInstruction(t *testing.T) {
	for _, in := range []string{"", "   ", "PAGE", `INCLUDETEXT "http://x.org/"`} {
		if _, _, ok := hyperlink(in); ok {
			t.Errorf("hyperlink(%q) reported a link", in)
		}
	}
}

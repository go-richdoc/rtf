// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package rtf

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"strconv"
	"strings"
	"testing"
)

// The expected strings are the whole tree, so a test that passes has pinned
// every node, not a field of one.
func TestBody(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"plain", `Hello\par`, `P[Hello]`},
		{"no final par", `Hello`, `P[Hello]`},
		{"two paragraphs", `a\par b\par`, `P[a]P[b]`},
		{"blank paragraphs dropped", `\par\par a\par `, `P[a]`},
		{"bold italic strike", `\b\i\strike x\strike0\i0\b0 y\par`, `P[<b><i><s>x</s></i></b>y]`},
		{"toggle zero", `\b x\b0 y\b1 z\par`, `P[<b>x</b>y<b>z</b>]`},
		{"italic word", `\i x\i0 \striked y\par`, `P[<i>x</i><s>y</s>]`},
		{"plain resets", `\b\i x\plain y\par`, `P[<b><i>x</i></b>y]`},
		{"hidden text", `a\v hidden\v0 b\par`, `P[ab]`},
		{"group scoping", `a{\b b}c\par`, `P[a<b>b</b>c]`},
		{"line", `a\line b\par`, `P[a<br>b]`},
		{"escaped", `\\ \{ \}\par`, `P[\ { }]`},
		{"backslash newline is par", "a\\\nb\\\r\nc", `P[a]P[b]P[c]`},
		{"newlines are not text", "a\r\nb\nc", `P[abc]`},
		{"sect", `a\sect b`, `P[a]P[b]`},
		{"unknown word", `a\zzz12 b\par`, `P[ab]`},
		{"unknown starred group", `a{\*\zzz secret}b\par`, `P[ab]`},
		{"known starred word kept", `a\*\b b\par`, `P[a<b>b</b>]`},
		{"control symbol unknown", `a\?b\par`, `P[ab]`},
		{"default font", `\deff1 \f2 a\plain b\par`, `P[ab]`},
		{"fonts", `{\fonttbl{\f0\fcharset0 Arial;}{\f1\fcharset204 X;}}\f1 a\par`, `P[a]`},
		{"pc pca mac ansi", `\pc \'80 \pca \'80 \mac \'80 \ansi \'80\par`, `P[Ç Ç Ä €]`},
		{"ansicpg", `\ansicpg1251 \'c0\par`, `P[А]`},
		{"ansicpg 932 pair", `\ansicpg932 \'82\'a0\par`, `P[あ]`},
		{"ansicpg 65001", `\ansicpg65001 \'e2\'82\'ac\par`, `P[€]`},
		{"font charset beats doc", `{\fonttbl{\f0\fcharset204 R;}}\f0 \'c0\par`, `P[А]`},
		{"fcharset unknown", `{\fonttbl{\f0\fcharset999 R;}}\f0 \'e9\par`, `P[é]`},
		{"hex in skipped group", `{\*\zzz \'e9}a\par`, `P[a]`},
		{"bad hex escape", `a\'zz b\par`, `P[azz b]`},
		{"short hex escape", `a\'e`, `P[ae]`},
		{"non-ascii bytes", "caf\xe9\\par", `P[café]`},
		{"non-ascii then ascii", "\xe9a\xe9\\par", `P[éaé]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dump(mustParse(t, tc.in)); got != tc.want {
				t.Errorf("%q\n got  %s\n want %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestUnicode(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"bmp with fallback", `\u8364?x\par`, "P[€x]"},
		{"negative", `\u-3 ?\par`, "P[\uFFFD]"},
		{"surrogate pair", `\u-10179?\u-8704?x\par`, "P[😀x]"},
		{"uc0", `\uc0\u8364 x\par`, "P[€x]"},
		{"uc2", `\uc2\u8364 ab c\par`, "P[€ c]"},
		{"skip word", `\u8364\'3fx\par`, "P[€x]"},
		{"skip control word counts as one", `\u8364\b x\par`, "P[€x]"},
		{"skip stops at brace", `\u8364{\b x}y\par`, "P[€<b>x</b>y]"},
		{"skip across text", `\uc3\u8364abcdef\par`, "P[€def]"},
		{"skip bin", `\u8364\bin1 Zx\par`, "P[€x]"},
		{"skip hex symbol", `\u8364\'41x\par`, "P[€x]"},
		{"skip symbol", `\u8364\~x\par`, "P[€x]"},
		{"lone high surrogate", `\u-10179?x\par`, "P[\uFFFDx]"},
		{"lone low surrogate", `\u-8704?x\par`, "P[\uFFFDx]"},
		{"high then high", `\u-10179?\u-10179?\u-8704?\par`, "P[\uFFFD😀]"},
		{"high then control", `\u-10179?\b x\par`, "P[<b>\uFFFDx</b>]"},
		{"u without number", `\u x\par`, "P[x]"},
		{"in uc skip, u ignored", `\uc2\u65\u66 \par`, "P[A]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dump(mustParse(t, tc.in)); got != tc.want {
				t.Errorf("%q\n got  %q\n want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParameters(t *testing.T) {
	// Parameters of any length are saturated, never overflowed, and a clamped
	// value must not change what is read.
	for _, in := range []string{
		`\b99999999999999999999999999999 x\par`,
		`\b-99999999999999999999999999999 x\par`,
		`\uc99999999999999999999 \u65 x\par`,
		`\ls99999999999999999999 x\par`,
		`\ilvl99999999999999999999\ls1 x\par`,
		`\itap99999999999999999999 x\par`,
		`\outlinelvl99999999999999999999 x\par`,
		`\pnstart99999999999999999999 x\par`,
		`\ansicpg99999999999999999999 x\par`,
		`\trowd\cellx99999999999999999999\intbl x\cell\row`,
	} {
		d, err := Parse([]byte(`{\rtf1 `+in+`}`), Limits{})
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if len(d.Blocks) == 0 && !strings.Contains(in, `\uc`) {
			t.Errorf("%q: nothing read", in)
		}
	}
	if got := dump(mustParse(t, `\b-0 x\par`)); got != `P[x]` {
		t.Errorf("negative zero turned bold on: %s", got)
	}
}

func TestHeadings(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"outline", `\pard\outlinelvl0 A\par\pard\outlinelvl2 B\par\pard\outlinelvl8 C\par`, `H1[A]H3[B]H6[C]`},
		{"outline out of range", `\outlinelvl9 A\par`, `P[A]`},
		{"outline reset by pard", `\outlinelvl0 A\par\pard B\par`, `H1[A]P[B]`},
		{"style by name", `{\stylesheet{\s0 Normal;}{\s1 heading 1;}{\s2 heading 3;}{\s3 heading 10;}{\s4 heading x;}}\pard\s1 A\par\pard\s2 B\par\pard\s3 C\par\s4 D\par`,
			`H1[A]H3[B]P[C]P[D]`},
		{"style outline", `{\stylesheet{\s1\outlinelvl1 Mine;}}\s1 A\par`, `H2[A]`},
		{"unknown style", `\s7 A\par`, `P[A]`},
		{"stylesheet text without style", `{\stylesheet{\f0 NoStyle;}}x\par`, `P[x]`},
		{"outlinelvl outside style", `{\stylesheet\outlinelvl3 ;}x\par`, `P[x]`},
		{"long style name", `{\stylesheet{\s1 ` + strings.Repeat("a", 1000) + `;}}x\par`, `P[x]`},
		{"style split by groups", `{\stylesheet{\s1 head{\b ing }1;}}\s1 A\par`, `H1[A]`},
		{"s restated", `{\stylesheet{\s1 heading 1;}{\s1 other;}}\s1 A\par`, `P[A]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dump(mustParse(t, tc.in)); got != tc.want {
				t.Errorf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestMeta(t *testing.T) {
	d := mustParse(t, `{\info{\title A title}{\author Jane}{\subject s}}x\par`)
	if d.Meta["title"] != "A title" || d.Meta["author"] != "Jane" || len(d.Meta) != 2 {
		t.Errorf("meta %v", d.Meta)
	}
	if d := mustParse(t, `{\info{\title   }}x\par`); d.Meta != nil {
		t.Errorf("blank title gave meta %v", d.Meta)
	}
	if d := mustParse(t, `{\info{\title X}}{\title not meta}x\par`); d.Meta["title"] != "X" {
		t.Errorf("meta %v", d.Meta)
	}
	long := strings.Repeat("t", 3*maxMeta)
	d = mustParse(t, `{\info{\title `+long+`}{\author `+long+`}}x\par`)
	if len(d.Meta["title"]) > maxMeta+len(long) || len(d.Meta["title"]) == 0 {
		t.Errorf("title length %d", len(d.Meta["title"]))
	}
	if d := mustParse(t, `{\info{\title \'e9}}x\par`); d.Meta["title"] != "é" {
		t.Errorf("hex in title: %q", d.Meta["title"])
	}
}

func TestSkippedDestinations(t *testing.T) {
	for _, in := range []string{
		`{\colortbl;\red0\green0\blue0;}x\par`,
		`{\header h}x\par`,
		`{\footer h}x\par`,
		`{\*\annotation c}x\par`,
		`{\shp{\sp s}}x\par`,
		`{\*\listtext 1.\tab}x\par`,
		`{\pntext 1.\tab}x\par`,
		`{\*\generator Word}x\par`,
	} {
		if got := dump(mustParse(t, in)); got != `P[x]` {
			t.Errorf("%q: %s", in, got)
		}
	}
	// A skipped group does not lose its closing brace to a \bin payload.
	if got := dump(mustParse(t, `{\*\zzz \bin3 }}}x}y\par`)); got != `P[y]` {
		t.Errorf("bin in skipped group: %s", got)
	}
}

func TestObject(t *testing.T) {
	in := `before {\object\objemb{\*\objclass Excel.Sheet}{\*\objdata 0105}{\result {\pict\pngblip 89}shown}} after\par`
	if got := dump(mustParse(t, in)); got != `P[before shown after]` {
		t.Errorf("object: %s", got)
	}
	if got := dump(mustParse(t, `{\object{\object x}{\result y}}\par`)); got != `P[y]` {
		t.Errorf("nested object: %s", got)
	}
	if got := dump(mustParse(t, `{\object{\*\objdata 0105}}z\par`)); got != `P[z]` {
		t.Errorf("object without result: %s", got)
	}
}

func TestLinks(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"http", `{\field{\*\fldinst HYPERLINK "http://a.example/x"}{\fldrslt \b text}}\par`, `P[<a http://a.example/x ""><b>text</b></a>]`},
		{"fragment", `{\field{\*\fldinst HYPERLINK "http://a.example/x" \\l "frag"}{\fldrslt t}}\par`, `P[<a http://a.example/x#frag "">t</a>]`},
		{"only fragment", `{\field{\*\fldinst HYPERLINK \\l "frag"}{\fldrslt t}}\par`, `P[<a #frag "">t</a>]`},
		{"tooltip", `{\field{\*\fldinst HYPERLINK "http://a" \\o "tip"}{\fldrslt t}}\par`, `P[<a http://a "tip">t</a>]`},
		{"target switch", `{\field{\*\fldinst HYPERLINK "http://a" \\t "_blank" \\m \\n}{\fldrslt t}}\par`, `P[<a http://a "">t</a>]`},
		{"switch without argument", `{\field{\*\fldinst HYPERLINK "http://a" \\o}{\fldrslt t}}\par`, `P[<a http://a "">t</a>]`},
		{"bare url", `{\field{\*\fldinst hyperlink mailto:a@b.example}{\fldrslt t}}\par`, `P[<a mailto:a@b.example "">t</a>]`},
		{"unterminated quote", `{\field{\*\fldinst HYPERLINK "http://a}{\fldrslt t}}\par`, `P[<a http://a "">t</a>]`},
		{"javascript", `{\field{\*\fldinst HYPERLINK "javascript:alert(1)"}{\fldrslt t}}\par`, `P[t]`},
		{"file", `{\field{\*\fldinst HYPERLINK "file:///etc/passwd"}{\fldrslt t}}\par`, `P[t]`},
		{"data", `{\field{\*\fldinst HYPERLINK "data:text/html,x"}{\fldrslt t}}\par`, `P[t]`},
		{"unc", `{\field{\*\fldinst HYPERLINK "\\\\host\\share"}{\fldrslt t}}\par`, `P[t]`},
		{"protocol relative", `{\field{\*\fldinst HYPERLINK "//host/x"}{\fldrslt t}}\par`, `P[t]`},
		{"control char", "{\\field{\\*\\fldinst HYPERLINK \"http://a\x01b\"}{\\fldrslt t}}\\par", `P[t]`},
		{"relative", `{\field{\*\fldinst HYPERLINK "docs/a.html?x:y"}{\fldrslt t}}\par`, `P[<a docs/a.html?x:y "">t</a>]`},
		{"no url", `{\field{\*\fldinst HYPERLINK}{\fldrslt t}}\par`, `P[t]`},
		{"other field keeps result", `{\field{\*\fldinst PAGE}{\fldrslt 7}}\par`, `P[7]`},
		{"empty field", `{\field}x\par`, `P[x]`},
		{"empty instruction", `{\field{\*\fldinst }{\fldrslt 7}}\par`, `P[7]`},
		{"empty result", `{\field{\*\fldinst HYPERLINK "http://a"}{\fldrslt }}x\par`, `P[x]`},
		{"fldinst outside field", `{\fldinst HYPERLINK "http://a"}x\par`, `P[x]`},
		{"field in skipped", `{\header{\field{\*\fldinst HYPERLINK "http://a"}{\fldrslt t}}}x\par`, `P[x]`},
		{"hex in instruction", `{\field{\*\fldinst HYPERLINK "http://a/\'e9"}{\fldrslt t}}\par`, `P[<a http://a/é "">t</a>]`},
		{"instruction capped", `{\field{\*\fldinst ` + strings.Repeat("x", 3*maxInst) + `}{\fldrslt t}}\par`, `P[t]`},
		{"text before and after", `a {\field{\*\fldinst HYPERLINK "http://a"}{\fldrslt t}} b\par`, `P[a <a http://a "">t</a> b]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// \\ in these raw strings is the RTF escape for one backslash.
			if got := dump(mustParse(t, tc.in)); got != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestSafeURL(t *testing.T) {
	for u, want := range map[string]bool{
		"https://a.example": true, "HTTP://A": true, "ftp://a": true, "ftps://a": true,
		"mailto:a@b": true, "tel:+33": true, "news:x": true, "#frag": true,
		"rel/path": true, "./x": true, "?q=a:b": true, "a/b:c": true, "plain": true,
		"javascript:x": false, "vbscript:x": false, "data:x": false, "file:x": false,
		"//x": false, `a\b`: false, "a\x00b": false, "a\x7fb": false, "a\nb": false,
		"jav\tascript:x": false, "": true,
	} {
		if got := safeURL(u); got != want {
			t.Errorf("safeURL(%q) = %v", u, got)
		}
	}
}

func TestFootnotesAndBookmarks(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"footnote", `text{\footnote{\b note} body}\par`, `P[text<fn:P[<b>note</b> body]>]`},
		{"footnote resets formatting", `\b a{\footnote n}\par`, `P[<b>a</b><fn:P[n]>]`},
		{"empty footnote", `a{\footnote }\par`, `P[a]`},
		{"footnote two paragraphs", `a{\footnote x\par y}\par`, `P[a<fn:P[x]P[y]>]`},
		{"footnote in skipped", `{\header {\footnote n}}a\par`, `P[a]`},
		{"bookmark", `a{\*\bkmkstart mark}b\par`, `P[a<#mark>b]`},
		{"bookmark gobTack", `{\*\bkmkstart _GoBack}a\par`, `P[a]`},
		{"bookmark unnamed", `{\*\bkmkstart }a\par`, `P[a]`},
		{"bookmark end skipped", `a{\*\bkmkend mark}b\par`, `P[ab]`},
		{"bookmark name capped", `{\*\bkmkstart ` + strings.Repeat("n", 2000) + `}a\par`, ``},
		{"page", `a\page b\par`, `P[a]HRP[b]`},
		{"page in skipped", `{\header \page}a\par`, `P[a]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := dump(mustParse(t, tc.in))
			if tc.want == "" {
				if !strings.HasPrefix(got, "P[<#nnn") || !strings.HasSuffix(got, ">a]") {
					t.Errorf("got %.40s", got)
				}
				return
			}
			if got != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

const listTable = `{\*\listtable
{\list\listtemplateid1{\listlevel\levelnfc23\levelstartat1{\leveltext \'01\u8226 ;}}{\listlevel\levelnfc0\levelstartat3}\listid10}
{\list\listid20{\listlevel\levelnfc0\levelstartat5}{\listlevel\levelnfc23}}
{\list\listid30{\listlevel\levelnfc255}}
}{\*\listoverridetable{\listoverride\listid10\listoverridecount0\ls1}{\listoverride\listid20\ls2}{\listoverride\listid30\ls3}{\listoverride\ls9}}`

func TestLists(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"bullets", listTable + `\ls1 a\par\ls1 b\par\pard c\par`, `UL{P[a],P[b]}P[c]`},
		{"numbered start", listTable + `\ls2 a\par\ls2 b\par`, `OL5{P[a],P[b]}`},
		{"numbering continues", listTable + `\ls2 a\par\pard x\par\ls2 b\par`, `OL5{P[a]}P[x]OL6{P[b]}`},
		{"nested", listTable + `\ls1 a\par\ilvl1 b\par c\par\pard\ls1 d\par`, `UL{P[a]OL3{P[b],P[c]},P[d]}`},
		{"kind change", listTable + `\ls1 a\par\ls2 b\par`, `UL{P[a]}OL5{P[b]}`},
		{"255 is a bullet", listTable + `\ls3 a\par\ls1 b\par`, `UL{P[a],P[b]}`},
		{"deeper then shallower twice", listTable + `\ls2 a\par\ilvl1 b\par\pard\ls2 c\par`, `OL5{P[a]UL{P[b]},P[c]}`},
		{"unknown list is bullet", `\ls77 a\par`, `UL{P[a]}`},
		{"ls without definition for level", listTable + `\ls3\ilvl4 a\par`, `UL{P[a]}`},
		{"nested first", listTable + `\ls1\ilvl1 a\par\pard\ls1 b\par`, `OL3{P[a]}UL{P[b]}`},
		{"old bullets", `{\*\pn\pnlvlblt\pnf1 \'b7}a\par b\par\pard c\par`, `UL{P[a],P[b]}P[c]`},
		{"old numbers", `{\*\pn\pnlvlbody\pndec\pnstart4}a\par b\par`, `OL4{P[a],P[b]}`},
		{"old numbers zero start", `{\*\pn\pnlvlbody\pnucrm\pnlcrm\pnucltr\pnlcltr}a\par`, `OL1{P[a]}`},
		{"heading beats list", listTable + `\ls1\outlinelvl0 a\par`, `H1[a]`},
		{"list after heading", listTable + `\outlinelvl0 h\par\pard\ls1 a\par`, `H1[h]UL{P[a]}`},
		{"override mapping", `{\*\listoverridetable{\listoverride\ls4\listid40}}\ls4 a\par`, `UL{P[a]}`},
		{"pn outside body", `{\header {\*\pn\pnlvlblt}}a\par`, `P[a]`},
		{"list levels capped", `{\*\listtable{\list\listid1` + strings.Repeat(`{\listlevel\levelnfc0\levelstartat2}`, 12) + `}}{\*\listoverridetable{\listoverride\listid1\ls1}}\ls1 a\par`, `OL2{P[a]}`},
		{"list level outside list", `\levelnfc3\levelstartat2 a\par`, `P[a]`},
		{"listid outside list", `\listid5 a\par`, `P[a]`},
		{"list outside table", `\list a\par\listoverride b\par\listlevel c\par`, `P[a]P[b]P[c]`},
		{"ls in override only counts when ls then id", `{\*\listoverridetable{\listoverride\ls0\listid50}}\ls0 a\par`, `P[a]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dump(mustParse(t, tc.in)); got != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestTables(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"simple", `\trowd\cellx1000\cellx2000\intbl A\cell B\cell\row\pard after\par`, `T{A|B;}P[after]`},
		{"two rows", `\trowd\cellx1\cellx2\intbl a\cell b\cell\row\trowd\cellx1\cellx2\intbl c\cell d\cell\row`, `T{a|b;c|d;}`},
		{"header row", `\trowd\trhdr\cellx1\cellx2\intbl a\cell b\cell\row\trowd\cellx1\cellx2\intbl c\cell d\cell\row`, `T{h:a|b;c|d;}`},
		{"second header is a row", `\trowd\trhdr\cellx1\intbl a\cell\row\trowd\trhdr\cellx1\intbl b\cell\row`, `T{h:a;b;}`},
		{"hmerge", `\trowd\clmgf\cellx1\clmrg\cellx2\cellx3\intbl a\cell \cell c\cell\row`, `T{a/c2|c;}`},
		{"hmerge continuation first", `\trowd\clmrg\cellx1\cellx2\intbl a\cell b\cell\row`, `T{a|b;}`},
		{"vmerge", `\trowd\clvmgf\cellx1\cellx2\intbl a\cell b\cell\row\trowd\clvmrg\cellx1\cellx2\intbl \cell c\cell\row\trowd\clvmrg\cellx1\cellx2\intbl \cell d\cell\row`, `T{a/r3|b;c;d;}`},
		{"vmerge without origin", `\trowd\clvmrg\cellx1\intbl a\cell\row`, `T{a;}`},
		{"vmerge in header is plain", `\trowd\trhdr\clvmgf\cellx1\intbl a\cell\row\trowd\clvmrg\cellx1\intbl b\cell\row`, `T{h:a;b;}`},
		{"table ended by paragraph", `\trowd\cellx1\intbl a\cell\row\pard b\par\trowd\cellx1\intbl c\cell\row`, `T{a;}P[b]T{c;}`},
		{"unterminated row", `\trowd\cellx1\cellx2\intbl a\cell b\cell`, `T{a|b;}`},
		{"unterminated cell", `\trowd\cellx1\intbl a`, `P[a]`},
		{"empty row", `\trowd\cellx1\row x\par`, `P[x]`},
		{"row without cells", `\row x\par`, `P[x]`},
		{"more cells than defs", `\trowd\cellx1\intbl a\cell b\cell\row`, `T{a|b;}`},
		{"multi paragraph cell", `\trowd\cellx1\intbl a\par b\cell\row`, `T{a<br>b;}`},
		{"list in cell", listTable + `\trowd\cellx1\intbl\ls1 a\par\ls1 b\cell\row`, `T{a<br>b;}`},
		{"heading in cell", `\trowd\cellx1\intbl\outlinelvl0 a\par b\cell\row`, `T{a<br>b;}`},
		{"page in cell", `\trowd\cellx1\intbl a\par\page b\cell\row`, `T{a<br>b;}`},
		{"row ends open cell", `\trowd\cellx1\intbl a\row`, `T{a;}`},
		{"cell at depth two", `\trowd\cellx1\itap2 a\cell \itap1 b\cell\row`, `T{a<br>b;}`},
		{"nested", `\trowd\cellx1\intbl\itap1 a\nestcell\itap2 n1\nestcell\itap2\nestrow\itap1 z\cell\row`, `T{a<br>n1<br>z;}`},
		{"nested trowd ignored", `\trowd\cellx1\intbl\itap2\trowd\cellx5 n\nestcell\nestrow\itap1 z\cell\row`, `T{n<br>z;}`},
		{"cell in skipped", `{\header \cell\row}x\par`, `P[x]`},
		{"itap resets intbl", `\itap0\intbl x\par`, `P[x]`},
		{"cell definitions only", `\trowd\cellx1\cellx2\row x\par`, `P[x]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dump(mustParse(t, tc.in)); got != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

// A real 1x1 PNG and JPEG: the reader reads their headers, not only the magic.
var (
	tinyPNG  = encodeImage(func(w *bytes.Buffer, m image.Image) { _ = png.Encode(w, m) })
	tinyJPEG = encodeImage(func(w *bytes.Buffer, m image.Image) { _ = jpeg.Encode(w, m, nil) })
)

func encodeImage(enc func(*bytes.Buffer, image.Image)) []byte {
	var b bytes.Buffer
	enc(&b, image.NewGray(image.Rect(0, 0, 1, 1)))
	return b.Bytes()
}

func TestPictures(t *testing.T) {
	hx := func(b []byte) string { return hex.EncodeToString(b) }
	for _, tc := range []struct{ name, in, want string }{
		{"png", `{\pict\pngblip\picwgoal2000\pichgoal1000 ` + hx(tinyPNG) + `}\par`, `P[<img 100pt 50pt 0>]`},
		{"jpeg scale", `{\pict\jpegblip\picscalex50\picscaley50 ` + hx(tinyJPEG) + `}\par`, `P[<img   50>]`},
		{"jpeg scale 100", `{\pict\jpegblip\picscalex100\picscaley100 ` + hx(tinyJPEG) + `}\par`, `P[<img   0>]`},
		{"scale differs", `{\pict\jpegblip\picscalex50\picscaley60 ` + hx(tinyJPEG) + `}\par`, `P[<img   0>]`},
		{"goal scaled", `{\pict\pngblip\picwgoal2000\pichgoal1000\picscalex50\picscaley200 ` + hx(tinyPNG) + `}\par`, `P[<img 50pt 100pt 0>]`},
		{"hex with line breaks", "{\\pict\\pngblip\r\n" + hx(tinyPNG[:6]) + "\r\n" + hx(tinyPNG[6:]) + "\r\n}\\par", `P[<img   0>]`},
		{"spaces inside hex", `{\pict\pngblip ` + hx(tinyPNG[:4]) + ` ` + hx(tinyPNG[4:]) + `}\par`, `P[<img   0>]`},
		{"odd digit left over", `{\pict\pngblip ` + hx(tinyPNG) + `a}\par`, `P[<img   0>]`},
		{"wrong magic dropped", `{\pict\pngblip ` + hx(tinyJPEG) + `}\par`, ``},
		{"emf dropped", `{\pict\emfblip ` + hx(tinyPNG) + `}x\par`, `P[x]`},
		{"no type dropped", `{\pict ` + hx(tinyPNG) + `}x\par`, `P[x]`},
		{"hidden dropped", `\v{\pict\pngblip ` + hx(tinyPNG) + `}\v0 x\par`, `P[x]`},
		{"shppict", `{\*\shppict{\pict\pngblip ` + hx(tinyPNG) + `}}\par`, `P[<img   0>]`},
		{"nonshppict skipped", `{\*\nonshppict{\pict\pngblip ` + hx(tinyPNG) + `}}x\par`, `P[x]`},
		{"pict in skipped", `{\header{\pict\pngblip ` + hx(tinyPNG) + `}}x\par`, `P[x]`},
		{"picture words outside a pict", `\pngblip\jpegblip\picwgoal1\pichgoal1\picscalex1\picscaley1 x\par`, `P[x]`},
		{"nested group in pict", `{\pict\pngblip{\*\blipuid 1234}` + hx(tinyPNG) + `}\par`, `P[<img   0>]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dump(mustParse(t, tc.in)); got != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
	// The image is a data URL of exactly the bytes that were read.
	d := mustParse(t, `{\pict\pngblip `+hx(tinyPNG)+`}\par`)
	if len(d.Blocks) != 1 {
		t.Fatalf("blocks %v", d.Blocks)
	}
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(tinyPNG)
	if got := imageURL(t, d); got != want {
		t.Errorf("url %q want %q", got, want)
	}
	// \bin carries the same bytes raw, braces and all.
	raw := append(bytes.Clone(tinyPNG), '{', '}', '\\')
	in := append([]byte(`{\rtf1{\pict\pngblip\bin`+strconv.Itoa(len(raw))+` `), raw...)
	in = append(in, []byte(`}x\par}`)...)
	d, err := Parse(in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	want = "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
	if got := imageURL(t, d); got != want {
		t.Errorf("bin url %q want %q", got, want)
	}
}

func TestBin(t *testing.T) {
	// \bin outside a picture is skipped, braces and all.
	in := []byte(`{\rtf1 a{\*\zzz\bin4 }}{}x\par}`)
	d, err := Parse(in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got := dump(d); got != `P[a]` {
		t.Errorf("got %s", got)
	}
	// \bin without a number is not a count.
	if got := dump(mustParse(t, `a\bin b\par`)); got != `P[ab]` {
		t.Errorf("bare bin: %s", got)
	}
	for _, tc := range []struct {
		name, in string
		lim      Limits
		want     error
	}{
		{"negative", `{\rtf1\bin-1 x}`, Limits{}, ErrSyntax},
		{"past the input", `{\rtf1\bin2147483647 x}`, Limits{}, ErrSyntax},
		{"saturated", `{\rtf1\bin99999999999999999999 x}`, Limits{}, ErrSyntax},
		{"one past the input", `{\rtf1\bin4 xx}`, Limits{}, ErrSyntax},
		{"past MaxBin", `{\rtf1\bin8 12345678}`, Limits{MaxBin: 7}, ErrLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.in), tc.lim)
			if !errors.Is(err, tc.want) {
				t.Errorf("err %v, want %v", err, tc.want)
			}
		})
	}
	// Exactly MaxBin is allowed, and exactly the rest of the input is allowed.
	if _, err := Parse([]byte(`{\rtf1\bin7 1234567}`), Limits{MaxBin: 7}); err != nil {
		t.Errorf("exactly MaxBin: %v", err)
	}
	if _, err := Parse([]byte(`{\rtf1\bin2 xx`), Limits{}); err != nil {
		t.Errorf("exactly the rest: %v", err)
	}
}

func TestLimits(t *testing.T) {
	deep := func(n int) string { return `{\rtf1` + strings.Repeat(`{`, n) + `x` + strings.Repeat(`}`, n) + `}` }
	if _, err := Parse([]byte(deep(5)), Limits{MaxDepth: 6}); err != nil {
		t.Errorf("depth at the limit: %v", err)
	}
	if _, err := Parse([]byte(deep(6)), Limits{MaxDepth: 6}); !errors.Is(err, ErrLimit) {
		t.Errorf("depth over the limit: %v", err)
	}
	if _, err := Parse([]byte(`{\rtf1 abc}`), Limits{MaxInput: 10}); !errors.Is(err, ErrLimit) {
		t.Errorf("input over: %v", err)
	}
	if _, err := Parse([]byte(`{\rtf1 ab}`), Limits{MaxInput: 10}); err != nil {
		t.Errorf("input at: %v", err)
	}
	if _, err := Parse([]byte(`{\rtf1 `+strings.Repeat("a", 11)+`}`), Limits{MaxText: 10}); !errors.Is(err, ErrLimit) {
		t.Errorf("text over: %v", err)
	}
	if _, err := Parse([]byte(`{\rtf1 `+strings.Repeat("a", 10)+`}`), Limits{MaxText: 10}); err != nil {
		t.Errorf("text at: %v", err)
	}
	if _, err := Parse([]byte(`{\rtf1 `+strings.Repeat(`a\par `, 20)+`}`), Limits{MaxNodes: 10}); !errors.Is(err, ErrLimit) {
		t.Errorf("nodes over: %v", err)
	}
	hx := strings.Repeat("ab", 11)
	if _, err := Parse([]byte(`{\rtf1{\pict\pngblip `+hx+`}}`), Limits{MaxPict: 10}); !errors.Is(err, ErrLimit) {
		t.Errorf("hex picture over: %v", err)
	}
	if _, err := Parse([]byte(`{\rtf1{\pict\pngblip `+hx[:20]+`}}`), Limits{MaxPict: 10}); err != nil {
		t.Errorf("hex picture at: %v", err)
	}
	if _, err := Parse([]byte(`{\rtf1{\pict\pngblip\bin6 123456}{\pict\pngblip\bin6 123456}}`), Limits{MaxPict: 10}); err != nil {
		t.Errorf("two pictures each at the limit: %v", err)
	}
	// MaxPict applies to \bin inside one picture, across chunks.
	if _, err := Parse([]byte(`{\rtf1{\pict\pngblip\bin6 123456\bin6 123456}}`), Limits{MaxPict: 10}); !errors.Is(err, ErrLimit) {
		t.Errorf("bin chunks over: %v", err)
	}
	// An error is the first one, and it is a refusal, never a partial document.
	d, err := Parse([]byte(`{\rtf1 `+strings.Repeat("a", 11)+`}`), Limits{MaxText: 10})
	if d != nil || err == nil {
		t.Errorf("partial result %v %v", d, err)
	}
	// Defaults are filled in field by field.
	l := Limits{MaxDepth: 3}.orDefaults()
	if l.MaxDepth != 3 || l.MaxInput != defaultMaxInput || l.MaxNodes != defaultMaxNodes {
		t.Errorf("orDefaults %+v", l)
	}
}

func TestNotRTF(t *testing.T) {
	for _, in := range []string{"", "plain", "{\\rt", "<html>", "{rtf1}", "\\rtf1"} {
		if _, err := Parse([]byte(in), Limits{}); !errors.Is(err, ErrNotRTF) {
			t.Errorf("%q: %v", in, err)
		}
	}
	for _, in := range []string{"\xef\xbb\xbf{\\rtf1 a}", "  \r\n{\\rtf1 a}", "{\\rtf1 a"} {
		d, err := Parse([]byte(in), Limits{})
		if err != nil || dump(d) != "P[a]" {
			t.Errorf("%q: %v %v", in, d, err)
		}
	}
}

func TestUnbalanced(t *testing.T) {
	for in, want := range map[string]string{
		`{\rtf1 a}}}b`:        `P[a]`,
		`{\rtf1 {{{a`:         `P[a]`,
		`{\rtf1 {\b a`:        `P[<b>a</b>]`,
		`{\rtf1 {\footnote n`: `P[<fn:P[n]>]`,
		`{\rtf1 {\field{\*\fldinst HYPERLINK "http://a"}{\fldrslt t`: `P[<a http://a "">t</a>]`,
		`{\rtf1 {\pict\pngblip 89`:                                   ``,
		`{\rtf1 a\`:                                                  `P[a]`,
	} {
		d, err := Parse([]byte(in), Limits{})
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got := dump(d); got != want {
			t.Errorf("%q: got %s want %s", in, got, want)
		}
	}
}

func decode(cp int, b []byte) string {
	return string(newReader(nil, Limits{}.orDefaults()).decode(cp, b))
}

func TestDecode(t *testing.T) {
	// A code page the table does not know falls back to the default.
	if got := decode(12345, []byte{0xe9}); got != "é" {
		t.Errorf("unknown cp: %q", got)
	}
	// A broken double-byte sequence is replaced, never dropped silently.
	if got := decode(932, []byte{0x82}); !strings.Contains(got, "\uFFFD") {
		t.Errorf("truncated sjis: %q", got)
	}
	if got := decode(65001, []byte{0xff, 'a'}); got != "\uFFFDa" {
		t.Errorf("bad utf-8: %q", got)
	}
	for cp := range codePage {
		if cp == 65001 {
			continue
		}
		if got := decode(cp, []byte{'A'}); got != "A" {
			t.Errorf("cp %d: %q", cp, got)
		}
	}
	for cs, cp := range charsetCP {
		if _, ok := codePage[cp]; !ok && cp != 65001 {
			t.Errorf("charset %d names cp %d, which has no decoder", cs, cp)
		}
	}
}

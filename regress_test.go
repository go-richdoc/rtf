// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package rtf

import (
	"encoding/hex"
	"errors"
	"runtime"
	"strings"
	"testing"
)

// The tests here are the review of v0.1.0 made permanent: each input is one a
// reviewer sent at v0.1.0, and each assertion failed on it.

// allocated is the bytes Parse allocated, in total, on in. The figure is for
// the whole process, which is idle between sequential tests: a bound with an
// order of magnitude of room, not a measurement.
func allocated(in string, lim Limits) (uint64, error) {
	runtime.GC()
	var a, b runtime.MemStats
	runtime.ReadMemStats(&a)
	_, err := Parse([]byte(in), lim)
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc, err
}

// The bytes of input a limit lets Parse read before it refuses are what the
// amplification bound is applied to, not the bytes of an input far larger.
// (The field instruction, the metadata and the high-byte inputs that were
// absolute figures here are cases of TestAmplification.)
func atMostPerByte(t *testing.T, what string, got uint64, read int) {
	t.Helper()
	if got > uint64(maxAmplification*read+fixedAllocation) {
		t.Errorf("%s allocated %d bytes for the %d it read, more than %dx", what, got, read, maxAmplification)
	}
}

// Every node is counted when it is made, so MaxNodes bounds memory: 12 bytes of
// input per two nodes, and v0.1.0 built all of them before it looked.
func TestNodesAreCountedWhenMade(t *testing.T) {
	in := `{\rtf1\ansi ` + strings.Repeat(`{\b a}{\i a}`, 20_000) + `}`
	n, err := allocated(in, Limits{MaxNodes: 1 << 16})
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("err = %v, want ErrLimit", err)
	}
	// Twelve bytes make two runs and a wrapper for each: four nodes. The copy of
	// the input that allocated makes is counted too, as it is in the table.
	atMostPerByte(t, "differently formatted runs without a paragraph", n, 3*(1<<16)+len(in))
}

// A run of text that is whole in the input is cut to the cap, not let in
// because the cap was checked before it.
func TestMetadataCapsApplyAfterAppending(t *testing.T) {
	long := strings.Repeat("a", 1<<20)
	d := mustParse(t, `{\info{\title `+long+`}{\author `+long+`}}x\par`)
	if got := len(d.Meta["title"]); got != maxMeta {
		t.Errorf("title is %d bytes, want %d", got, maxMeta)
	}
	if got := len(d.Meta["author"]); got != maxMeta {
		t.Errorf("author is %d bytes, want %d", got, maxMeta)
	}
	// Cut on a character boundary.
	d = mustParse(t, `{\info{\title `+strings.Repeat("\\u233?", maxMeta)+`}}x\par`)
	if got := d.Meta["title"]; len(got) > maxMeta || strings.Contains(got, "�") {
		t.Errorf("title cut inside a character: %d bytes, %q", len(got), got[len(got)-4:])
	}
}

func TestMetadataCountsAgainstMaxText(t *testing.T) {
	in := `{\rtf1{\info{\title ` + strings.Repeat("a", 200) + `}}}`
	if _, err := Parse([]byte(in), Limits{MaxText: 100}); !errors.Is(err, ErrLimit) {
		t.Errorf("err = %v, want ErrLimit", err)
	}
	// MaxText is met, not exceeded, by a title of exactly that size.
	in = `{\rtf1{\info{\title ` + strings.Repeat("a", 100) + `}}}`
	if _, err := Parse([]byte(in), Limits{MaxText: 100}); err != nil {
		t.Errorf("100 bytes against MaxText 100: %v", err)
	}
}

// Bytes above 0x7F wait to be decoded; they are decoded in pieces, so the text
// limit is met long before a file of them has been held whole.
func TestHighBytesStopAtMaxText(t *testing.T) {
	in := `{\rtf1\ansi ` + strings.Repeat("\xe9", 32<<20) + `}`
	n, err := allocated(in, Limits{MaxText: 1 << 20})
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("err = %v, want ErrLimit", err)
	}
	atMostPerByte(t, "32 MiB of accents against a 1 MiB text limit", n, 1<<20+maxPend)
}

// A destination that is full queues no byte for decoding, however many come.
func TestFullDestinationQueuesNothing(t *testing.T) {
	r := newReader(nil, Limits{}.orDefaults())
	r.st.dest = dTitle
	r.title = make([]byte, maxMeta)
	for range 100 {
		r.addPend(0xe9)
	}
	if len(r.pend) != 0 {
		t.Errorf("%d bytes queued for a full title", len(r.pend))
	}
	r.title = r.title[:maxMeta-1]
	r.addPend(0xe9)
	if len(r.pend) != 1 {
		t.Errorf("%d bytes queued with room for one", len(r.pend))
	}
}

// grow makes room only when there is none, and then makes it by doubling, so
// that n appends cost a few copies and not n.
func TestGrow(t *testing.T) {
	s := make([]int, 3, 10)
	if g := grow(s, 7); &g[0] != &s[0] || cap(g) != 10 {
		t.Errorf("room for 7 more: reallocated (cap %d)", cap(g))
	}
	g := grow(s, 8)
	if &g[0] == &s[0] || len(g) != 3 || cap(g) < 11 {
		t.Errorf("no room for 8 more: len %d cap %d", len(g), cap(g))
	}
	if g := grow([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17}[:17:17], 1); cap(g) != 34 {
		t.Errorf("a full slice of 17 doubled to %d, want 34", cap(g))
	}
	if g := grow([]int(nil), 100); cap(g) != 100 || len(g) != 0 {
		t.Errorf("a large first request: len %d cap %d", len(g), cap(g))
	}
	if g := grow([]int(nil), 1); cap(g) != 16 {
		t.Errorf("a first small request: cap %d, want 16", cap(g))
	}
}

func TestCapAppend(t *testing.T) {
	r := newReader(nil, Limits{MaxText: 10}.orDefaults())
	if got := string(r.capAppend(nil, "héllo", 2)); got != "h" {
		t.Errorf("cut inside é: %q", got)
	}
	if got := string(r.capAppend([]byte("abc"), "def", 3)); got != "abc" {
		t.Errorf("full buffer took more: %q", got)
	}
	// Text that is not UTF-8 is cut at the cap, and a full buffer takes none.
	if got := r.capAppend(nil, "\x80x", 0); len(got) != 0 {
		t.Errorf("no room, got %q", got)
	}
	if got := string(r.capAppend(nil, "\x80\x80", 1)); got != "" {
		t.Errorf("cut before a continuation byte: %q", got)
	}
	if r.err != nil {
		t.Fatalf("nothing past the cap is counted: %v", r.err)
	}
	r.capAppend(nil, strings.Repeat("a", 11), 100)
	if !errors.Is(r.err, ErrLimit) {
		t.Errorf("err = %v, want ErrLimit", r.err)
	}
}

func TestStyleNamesAreCapped(t *testing.T) {
	// A name is read up to maxStyleName bytes, so what lies past them (here
	// the words that would make the style a heading) is not part of it.
	pad := strings.Repeat(" ", maxStyleName)
	for want, name := range map[string]string{
		`H1[x]`: `heading 1`,
		`P[x]`:  pad + `heading 1`,
	} {
		if got := dump(mustParse(t, `{\stylesheet{\s1 `+name+`;}}\pard\s1 x\par`)); got != want {
			t.Errorf("%.20q: got %s want %s", name, got, want)
		}
	}
	// Nor is a name that has no end copied whole into the next one.
	in := `{\rtf1{\stylesheet{\s1 ` + strings.Repeat("a", 1<<20) + `}}}`
	if _, err := Parse([]byte(in), Limits{}); err != nil {
		t.Fatal(err)
	}
}

func TestRegressionTrees(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		// The text after a row is after the table.
		{"text after a row", `\trowd\cellx1000\intbl A\cell\row B\par`, `T{A;}P[B]`},
		{"text after a row and pard", `\trowd\cellx1000\intbl A\cell\row\pard B\par`, `T{A;}P[B]`},
		// A page break in a cell leaves the cell whole.
		{"page in the middle of a cell", `\trowd\cellx1000\cellx2000\intbl A\page B\cell C\cell\row\pard after\par`, `T{AB|C;}P[after]`},
		// A high surrogate with no low half is U+FFFD, wherever it ends.
		{"high surrogate at the end", `x` + hiD83D, "P[x�]"},
		{"high surrogate at a group end", `{x` + hiD83D + `}` + loDE00 + `z\par`, "P[x��z]"},
		{"high surrogate pairs", hiD83D + loDE00 + `z\par`, "P[\U0001F600z]"},
		// \levelstartat0 stays 0.
		{"levelstartat 0", `{\listtable{\list{\listlevel\levelnfc0\levelstartat0}\listid7}}{\listoverridetable{\listoverride\listid7\ls1}}` +
			`\pard\ls1 one\par\pard\ls1 two\par\pard mid\par\pard\ls1 three\par`, `OL0{P[one],P[two]}P[mid]OL2{P[three]}`},
		{"levelstartat absent is 1", `{\listtable{\list{\listlevel\levelnfc0}\listid7}}{\listoverridetable{\listoverride\listid7\ls1}}\pard\ls1 a\par`, `OL1{P[a]}`},
		// An absolute path is not a link.
		{"absolute path", `{\field{\*\fldinst HYPERLINK "/etc/passwd"}{\fldrslt abs}}\par`, `P[abs]`},
		{"relative path", `{\field{\*\fldinst HYPERLINK "docs/a.html"}{\fldrslt rel}}\par`, `P[<a docs/a.html "">rel</a>]`},
		{"fragment", `{\field{\*\fldinst HYPERLINK "#top"}{\fldrslt up}}\par`, `P[<a #top "">up</a>]`},
		// Alignment is a class on the paragraph.
		{"alignment", `\qc a\par\pard b\par\qr c\par\qj d\par\ql e\par`, `P.align-center[a]P[b]P.align-right[c]P.align-justify[d]P[e]`},
		{"alignment in a list item", `{\*\pn\pnlvlblt}\qc a\par`, `UL{P.align-center[a]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dump(mustParse(t, tc.in)); got != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

// A PNG is its signature AND a header that decodes; a JPEG likewise.
func TestPicturesNeedAHeader(t *testing.T) {
	flipped := append([]byte(nil), tinyPNG...)
	flipped[19]++ // the width, inside IHDR's data: its CRC no longer holds
	for _, tc := range []struct {
		name string
		kind string
		data []byte
		keep bool
	}{
		{"png", `pngblip`, tinyPNG, true},
		{"signature only", `pngblip`, pngMagic, false},
		{"signature and junk", `pngblip`, append(append([]byte(nil), pngMagic...), 0, 0, 0, 13, 'I', 'H', 'D', 'R', 1, 2, 3), false},
		{"png with a bad header CRC", `pngblip`, flipped, false},
		{"jpeg", `jpegblip`, tinyJPEG, true},
		{"jpeg magic only", `jpegblip`, jpegMagic, false},
		{"jpeg magic and junk", `jpegblip`, append(append([]byte(nil), jpegMagic...), 0xe0, 1, 2), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := dump(mustParse(t, `{\pict\`+tc.kind+` `+hexString(tc.data)+`}x\par`))
			if tc.keep != strings.Contains(got, "<img") {
				t.Errorf("kept = %v, want %v: %s", !tc.keep, tc.keep, got)
			}
		})
	}
}

// The allocation count of the benchmark document is a ceiling: v0.1.0 made
// 568007 of them for a 1 MB file.
func TestAllocationsOfAMegabyte(t *testing.T) {
	in := benchDoc()
	n := testing.AllocsPerRun(3, func() {
		if _, err := Parse(in, Limits{}); err != nil {
			t.Fatal(err)
		}
	})
	if n > allocCeiling {
		t.Errorf("%.0f allocations for %d bytes, ceiling %d", n, len(in), allocCeiling)
	}
}

// 唵7? and 嚃2?: the two halves of U+1F600 as RTF writes them. The
// backslash is built, not written, so that no tool reads "唵" as an escape.
var (
	hiD83D = string(rune(92)) + "u55357?"
	loDE00 = string(rune(92)) + "u56832?"
)

func hexString(b []byte) string { return hex.EncodeToString(b) }

// allocCeiling is the ceiling TestAllocationsOfAMegabyte holds.
const allocCeiling = 250_000 // 236352 measured

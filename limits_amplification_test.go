// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package rtf

import (
	"errors"
	"fmt"
	"math"
	"runtime/debug"
	"strings"
	"testing"
)

// Under the DEFAULT Limits, Parse allocates at most maxAmplification bytes for
// each byte of input, plus fixedAllocation whatever the input: the code-page
// table of a first accent, the maps of a reader. Both are stated in the README.
// The worst measured over the sweep of TestAmplification is 65, on the densest
// run of formatted one-letter runs, just past a doubling of its slice; the
// constant is that with a margin of about a fifth.
const (
	maxAmplification = 80
	fixedAllocation  = 256 << 10
)

// amplificationCase builds an input of about n bytes.
type amplificationCase struct {
	name string
	make func(n int) string
}

func rep(unit string, n int) string { return strings.Repeat(unit, max(1, n/len(unit))) }

// The adversarial inputs: the probes of the two reviews of this package, each
// at several sizes, and one for every destination that keeps text.
var amplificationCases = []amplificationCase{
	{"F1 many long field instructions", func(n int) string {
		return `{\rtf1\ansi ` + rep(`{\field{\*\fldinst `+strings.Repeat(`\u65?`, 8200)+`}{\fldrslt x}}`, n) + `}`
	}},
	{"F2 alternating runs", func(n int) string { return `{\rtf1\ansi ` + rep(`{\b a}{\i a}`, n) + `}` }},
	{"F3 long title and author", func(n int) string {
		return `{\rtf1\ansi {\info{\title ` + strings.Repeat("a", n/2) + `}{\author ` + strings.Repeat("b", n/2) + `}}}`
	}},
	{"F4 high bytes in the body", func(n int) string { return `{\rtf1\ansi ` + strings.Repeat("\xe9", n) + `}` }},
	{"F4 double-byte code page", func(n int) string { return `{\rtf1\ansi\ansicpg932 ` + strings.Repeat("\x82\xa0", n/2) + `}` }},
	{"ASCII body", func(n int) string { return `{\rtf1\ansi ` + strings.Repeat("x", n) + `}` }},
	{"nested fields", nestedFields(1)},
	{"nested fields 16", nestedFields(16)},
	{"nested fields 100", nestedFields(100)},
	{"nested fields 1000", nestedFields(1000)},
	{"repeated short groups", func(n int) string { return `{\rtf1\ansi ` + rep(`{a}`, n) + `}` }},
	{"repeated short groups with format", func(n int) string { return `{\rtf1\ansi ` + rep(`{\b a}`, n) + `}` }},
	{"paragraphs", func(n int) string { return `{\rtf1\ansi ` + rep(`a\par `, n) + `}` }},
	{"table rows", func(n int) string { return `{\rtf1\ansi ` + rep(`\trowd\cellx1000\intbl a\cell\row `, n) + `}` }},
	{"list items", func(n int) string {
		return `{\rtf1\ansi {\*\listtable{\list\listid1{\listlevel\levelnfc0}}}{\*\listoverridetable{\listoverride\listid1\ls1}}` + rep(`\ls1 a\par `, n) + `}`
	}},
	{"footnotes", func(n int) string { return `{\rtf1\ansi ` + rep(`{\footnote a}`, n) + `}` }},
	{"footnotes with format", func(n int) string { return `{\rtf1\ansi ` + rep(`{\footnote\b a}`, n) + `}` }},
	{"cells without a row", func(n int) string { return `{\rtf1\ansi \trowd\intbl ` + rep(`\cell`, n) + `}` }},
	{"footnotes and cells", func(n int) string { return `{\rtf1\ansi ` + rep(`{\footnote a}\cell`, n) + `}` }},
	{"densest inlines", func(n int) string { return `{\rtf1\ansi \b\strike ` + rep(`\i a\i0 a`, n) + `}` }},
	{"deep groups", func(n int) string { return `{\rtf1` + rep(`{`, n) + `a` + rep(`}`, n) + `}` }},
	{"long bookmark name", func(n int) string { return `{\rtf1\ansi {\*\bkmkstart ` + strings.Repeat("a", n) + `}x\par}` }},
	{"long style name", func(n int) string { return `{\rtf1\ansi {\stylesheet{\s1 ` + strings.Repeat("a", n) + `;}}x\par}` }},
	{"high bytes in a title", func(n int) string { return `{\rtf1\ansi {\info{\title ` + strings.Repeat("\xe9", n) + `}}}` }},
	{"high bytes in a field instruction", func(n int) string {
		return `{\rtf1\ansi {\field{\*\fldinst ` + strings.Repeat("\xe9", n) + `}{\fldrslt x}}}`
	}},
	{"high bytes in a style name", func(n int) string {
		return `{\rtf1\ansi {\stylesheet{\s1 ` + strings.Repeat("\xe9", n) + `;}}x\par}`
	}},
	{"high bytes in a bookmark", func(n int) string { return `{\rtf1\ansi {\*\bkmkstart ` + strings.Repeat("\xe9", n) + `}x\par}` }},
	{"unicode escapes in the body", func(n int) string { return `{\rtf1\ansi ` + rep(`\u233?`, n) + `}` }},
	{"hex picture", func(n int) string { return `{\rtf1\ansi {\pict\pngblip ` + strings.Repeat("ab", n/2) + `}}` }},
	{"skipped destination", func(n int) string { return `{\rtf1\ansi {\*\unknown ` + strings.Repeat("a", n) + `}x}` }},
	{"hidden text", func(n int) string { return `{\rtf1\ansi \v ` + strings.Repeat("a", n) + `}` }},
}

// nestedFields is depth HYPERLINK fields opened one inside another, 4 times the
// size of text, and depth closings: a naive reader copies the text at every
// level.
func nestedFields(depth int) func(n int) string {
	return func(n int) string {
		return `{\rtf1\ansi ` + strings.Repeat(`{\field{\*\fldinst HYPERLINK "http://a"}{\fldrslt a`, depth) +
			strings.Repeat("x", n) + strings.Repeat(`b}}`, depth) + `}`
	}
}

// sweepSizes are the input sizes tried for every case: from 1 KiB to the top,
// at stepsPerOctave sizes to each doubling of the input. A slice that doubles
// leaves its worst ratio just past the doubling, and what doubles is a count of
// runs, cells or footnotes, not a number of bytes: sizes spaced a few percent
// apart put one just past every doubling of every case, where a table of round
// sizes would miss the point by luck.
func sweepSizes() []int {
	steps, top := 8, 8<<20
	if raceEnabled {
		// The race detector makes the sweep minutes long and does not change
		// what is allocated.
		steps, top = 1, 2<<20
	}
	var sizes []int
	for i := 0; ; i++ {
		n := int(1024 * math.Pow(2, float64(i)/float64(steps)))
		if n > top {
			return sizes
		}
		sizes = append(sizes, n)
	}
}

// regressionInputs are inputs that once broke the gate, at the size that did:
// the densest runs, 140,000 of them, 64.7 times their size when the
// constant was 64.
var regressionInputs = map[string]string{
	"densest runs, 140000": `{\rtf1\ansi \b\strike ` + strings.Repeat(`\i a\i0 a`, 140000) + `}`,
}

func TestAmplification(t *testing.T) {
	// The bomb tests read the memory the process holds, so this file sorts after
	// bomb_test.go, which Go runs first; and what the largest of these inputs
	// took is given back.
	t.Cleanup(debug.FreeOSMemory)
	check := func(t *testing.T, name, in string) float64 {
		got, err := allocated(in, Limits{})
		if err != nil && !errors.Is(err, ErrLimit) && !errors.Is(err, ErrSyntax) {
			t.Fatal(err)
		}
		// The ratio the gate bounds: what is allocated past the fixed part.
		ratio := max(0, float64(got)-fixedAllocation) / float64(len(in))
		if got > uint64(maxAmplification*len(in)+fixedAllocation) {
			t.Errorf("%s: %d bytes for %d: %.1fx the input past the fixed part, more than %dx and %d KiB", name, got, len(in), ratio, maxAmplification, fixedAllocation>>10)
		}
		return ratio
	}
	sizes := sweepSizes()
	worst, worstCase := 0.0, ""
	for _, tc := range amplificationCases {
		t.Run(tc.name, func(t *testing.T) {
			wr, wn := 0.0, 0
			for _, n := range sizes {
				in := tc.make(n)
				if r := check(t, fmt.Sprintf("%s/%d", tc.name, n), in); r > wr {
					wr, wn = r, len(in)
				}
			}
			t.Logf("AMP %-36s worst %6.1fx at %8d bytes of %d sizes", tc.name, wr, wn, len(sizes))
			if wr > worst {
				worst, worstCase = wr, tc.name
			}
		})
	}
	for name, in := range regressionInputs {
		t.Run(name, func(t *testing.T) { t.Logf("AMP %-36s %6.1fx", name, check(t, name, in)) })
	}
	t.Logf("AMP WORST %.1fx (%s) over %d sizes", worst, worstCase, len(sizes))
}

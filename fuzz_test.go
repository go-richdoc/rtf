// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package rtf

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// FuzzParse feeds the reader arbitrary bytes. It must not panic, must return
// either a document or an error and never both, and an error must be one of the
// three the package documents. Limits are small so that a hostile input is
// refused quickly. The corpus under testdata/fuzz is committed.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		`{\rtf1\ansi a\par}`,
		`{\rtf1{\field{\*\fldinst HYPERLINK "http://a"}{\fldrslt t}}}`,
		`{\rtf1{\pict\pngblip 89504e470d0a1a0a}}`,
		`{\rtf1{\pict\pngblip\bin3 abc}}`,
		`{\rtf1\trowd\cellx1\intbl a\cell\row}`,
		`{\rtf1{\*\listtable{\list\listid1{\listlevel\levelnfc0}}}{\*\listoverridetable{\listoverride\listid1\ls1}}\ls1 a\par}`,
		`{\rtf1\uc2荤ab\u-10179?\u-8704?}`,
		`{\rtf1\uc2\u` + `8364 ab\u` + `55357?\u` + `56832?}`,
		// the review of v0.1.0: an unpaired high surrogate at a group's end and
		// at the input's end, a level that starts at 0, a \u inside a field
		// instruction and inside a title
		`{\rtf1{x\u` + `55357?}\u` + `56832?z\par}`,
		`{\rtf1\u` + `55357?`,
		`{\rtf1{\listtable{\list{\listlevel\levelnfc0\levelstartat0}\listid7}}{\listoverridetable{\listoverride\listid7\ls1}}\ls1 a\par}`,
		`{\rtf1{\field{\*\fldinst \u` + `65?\u` + `65?\u` + `65?}{\fldrslt x}}{\info{\title \u` + `233?\u` + `233?}}}`,
		`{\rtf1{\footnote n}{\object{\result r}}\'e9}`,
		`{\rtf1 {\*\pn\pnlvlbody\pnstart3}a\par}`,
	} {
		f.Add([]byte(s))
	}
	files, _ := filepath.Glob("testdata/*/*.rtf")
	for _, p := range files {
		if b, err := os.ReadFile(p); err == nil {
			f.Add(b)
		}
	}
	lim := Limits{MaxInput: 1 << 20, MaxDepth: 32, MaxBin: 1 << 16, MaxPict: 1 << 16, MaxNodes: 1 << 14, MaxText: 1 << 18}
	f.Fuzz(func(t *testing.T, b []byte) {
		d, err := Parse(b, lim)
		if (d == nil) == (err == nil) {
			t.Fatalf("document %v and error %v", d, err)
		}
		if err != nil && !errors.Is(err, ErrLimit) && !errors.Is(err, ErrSyntax) && !errors.Is(err, ErrNotRTF) {
			t.Fatalf("undocumented error %v", err)
		}
	})
}

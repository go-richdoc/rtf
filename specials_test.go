// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package rtf

import "testing"

// The characters a control word stands for, by code point (RTF 1.9.1,
// "Special Characters").
func TestSpecialCharacters(t *testing.T) {
	for word, cp := range map[string]rune{
		`\tab`: 0x09, `\emdash`: 0x2014, `\endash`: 0x2013, `\bullet`: 0x2022,
		`\lquote`: 0x2018, `\rquote`: 0x2019, `\ldblquote`: 0x201C, `\rdblquote`: 0x201D,
		`\enspace`: 0x2002, `\emspace`: 0x2003, `\qmspace`: 0x2005,
		`\zwj`: 0x200D, `\zwnj`: 0x200C, `\~`: 0xA0, `\_`: 0x2011,
	} {
		d := mustParse(t, `x`+word+` y\par`)
		// A control word's delimiter is a space; a control symbol's is not.
		want := "P[x" + string(cp) + " y]"
		if word == `\tab` || len(word) > 2 {
			want = "P[x" + string(cp) + "y]"
		}
		if got := dump(d); got != want {
			t.Errorf("%s: got %q want %q", word, got, want)
		}
	}
}

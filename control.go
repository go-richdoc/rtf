// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package rtf

import (
	"fmt"
)

func isLetter(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }
func isDigit(c byte) bool  { return c >= '0' && c <= '9' }

func hexVal(c byte) int {
	switch {
	case isDigit(c):
		return int(c - '0')
	case c|0x20 >= 'a' && c|0x20 <= 'f':
		return int(c|0x20-'a') + 10
	}
	return -1
}

// control reads one control word or control symbol, which starts at a
// backslash.
func (r *reader) control() {
	src := r.src
	i := r.pos + 1
	if i >= len(src) {
		r.pos = len(src) // a lone trailing backslash
		return
	}
	c := src[i]
	if !isLetter(c) {
		r.pos = i + 1
		r.symbol(c)
		return
	}
	j := i
	for j < len(src) && isLetter(src[j]) {
		j++
	}
	name := src[i:j]
	has, neg, val := false, false, int64(0)
	if j < len(src) && (src[j] == '-' || isDigit(src[j])) {
		has = true
		if src[j] == '-' {
			neg = true
			j++
		}
		for j < len(src) && isDigit(src[j]) {
			// Saturate rather than overflow: a parameter may be any number of
			// digits and only the clamped value is meaningful.
			if val <= 1<<40 {
				val = val*10 + int64(src[j]-'0')
			}
			j++
		}
		if neg {
			val = -val
		}
	}
	if j < len(src) && src[j] == ' ' {
		j++ // the delimiter, not text
	}
	r.pos = j
	p := int(max(minParam, min(maxParam, val)))
	if string(name) == "bin" {
		// \binN is read whether or not it is being skipped: its payload is
		// not RTF and may contain braces.
		r.flushPend()
		r.bin(p)
		if r.skip > 0 {
			r.skip--
		}
		return
	}
	if r.skip > 0 {
		r.skip--
		return
	}
	r.flushPend()
	r.word(string(name), p, has)
}

// symbol handles a control symbol: a backslash and one non-letter.
func (r *reader) symbol(c byte) {
	if c == '\'' {
		// \'hh is a byte in the current code page.
		if r.pos+1 < len(r.src) {
			h, l := hexVal(r.src[r.pos]), hexVal(r.src[r.pos+1])
			if h >= 0 && l >= 0 {
				r.pos += 2
				if r.skip > 0 {
					r.skip--
					return
				}
				r.hexByte(byte(h<<4 | l))
				return
			}
		}
		return
	}
	if r.skip > 0 {
		r.skip--
		return
	}
	r.flushPend()
	switch c {
	case '*':
		r.star = true
		return
	case '\\', '{', '}':
		r.addString(string(rune(c)))
	case '~':
		r.addString(" ")
	case '_':
		r.addString("‑")
	case '\r', '\n':
		// backslash newline is a \par (RTF 1.9.1, "Control Symbol")
		r.word("par", 0, false)
	}
	r.star = false
}

// hexByte is the byte a \'hh names, queued with its neighbours.
func (r *reader) hexByte(b byte) {
	switch r.st.dest {
	case dBody, dFldInst, dStyleSheet, dTitle, dAuthor, dBookmark:
		r.pend = append(r.pend, b)
	}
}

// bin consumes the n bytes after \binN. The count is a 32-bit parameter, so it
// is checked against the input that remains BEFORE anything is allocated or
// skipped: a six-byte control word must not be able to name a gigabyte.
func (r *reader) bin(n int) {
	if n < 0 || n > len(r.src)-r.pos {
		r.fail(fmt.Errorf("%w: \\bin %d with %d bytes left", ErrSyntax, n, len(r.src)-r.pos))
		return
	}
	if n > r.lim.MaxBin {
		r.fail(limitErr("\\bin of %d bytes, the limit is %d", n, r.lim.MaxBin))
		return
	}
	b := r.src[r.pos : r.pos+n]
	r.pos += n
	if r.st.dest == dPict && r.st.pict != nil {
		r.st.pict.add(r, b)
	}
}

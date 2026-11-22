// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package rtf

import (
	"bytes"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// codePage is the table behind \ansicpgN and behind the code page a font's
// \fcharsetN implies. A code page this table does not know decodes as 1252,
// which is what every reader falls back to: wrong accents are a smaller loss
// than refusing the file.
//
// 65001 is UTF-8 and has no entry: [decode] passes it through.
var codePage = map[int]encoding.Encoding{
	437:   charmap.CodePage437,
	850:   charmap.CodePage850,
	852:   charmap.CodePage852,
	866:   charmap.CodePage866,
	874:   charmap.Windows874,
	932:   japanese.ShiftJIS,
	936:   simplifiedchinese.GBK,
	949:   korean.EUCKR,
	950:   traditionalchinese.Big5,
	1250:  charmap.Windows1250,
	1251:  charmap.Windows1251,
	1252:  charmap.Windows1252,
	1253:  charmap.Windows1253,
	1254:  charmap.Windows1254,
	1255:  charmap.Windows1255,
	1256:  charmap.Windows1256,
	1257:  charmap.Windows1257,
	1258:  charmap.Windows1258,
	10000: charmap.Macintosh,
}

const (
	cpUTF8    = 65001
	cpDefault = 1252
)

// charsetCP is the code page a font's \fcharsetN names (RTF 1.9.1, "Font
// Table": the charset is the Windows CHARSET value). Zero means "the
// document's own", which covers ANSI_CHARSET (0), DEFAULT_CHARSET (1) and
// SYMBOL_CHARSET (2): a symbol font's bytes are not text in any code page.
var charsetCP = map[int]int{
	77:  10000,
	128: 932,
	129: 949,
	134: 936,
	136: 950,
	161: 1253,
	162: 1254,
	163: 1258,
	177: 1255,
	178: 1256,
	186: 1257,
	204: 1251,
	222: 874,
	238: 1250,
	254: 437,
	255: 850,
}

// decode turns code-page bytes into text. Bytes that are not valid in the page
// become U+FFFD, as x/text decodes them.
//
// The decoder for a code page is built once per reader and reused: Bytes resets
// it on every call, and building one was a fifth of the allocations of a
// document with accents in it.
func (r *reader) decode(cp int, b []byte) []byte {
	if cp == cpUTF8 {
		return bytes.ToValidUTF8(b, []byte("�"))
	}
	if _, ok := codePage[cp]; !ok {
		cp = cpDefault
	}
	dec, ok := r.decoders[cp]
	if !ok {
		dec = codePage[cp].NewDecoder()
		r.decoders[cp] = dec
	}
	// Every table above is a charmap or a CJK decoder; none returns an error
	// from Bytes except through U+FFFD substitution.
	out, _ := dec.Bytes(b)
	return out
}

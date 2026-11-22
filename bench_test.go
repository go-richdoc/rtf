// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package rtf

import (
	"os"
	"strings"
	"testing"
)

// A document of about 1 MB made of what real files hold in bulk: styled
// paragraphs, accents as \'hh, \uN words, a list and a table.
func benchDoc() []byte {
	var b strings.Builder
	b.WriteString(`{\rtf1\ansi\ansicpg1252\deff0{\fonttbl{\f0 Times New Roman;}}`)
	for b.Len() < 1<<20 {
		b.WriteString(`\pard\f0\fs24 Plain text with {\b bold}, {\i italic} and caf\'e9 荤? and ☈5?♁2?.\par` + "\n")
		b.WriteString(`\pard\intbl a\cell b\cell\row` + "\n")
	}
	b.WriteString(`}`)
	return []byte(b.String())
}

func BenchmarkParseSynthetic(b *testing.B) {
	in := benchDoc()
	b.SetBytes(int64(len(in)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Parse(in, Limits{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseTextutil(b *testing.B) {
	in, err := os.ReadFile("testdata/textutil/sample.rtf")
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(in)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Parse(in, Limits{}); err != nil {
			b.Fatal(err)
		}
	}
}

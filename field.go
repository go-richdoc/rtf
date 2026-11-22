// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package rtf

import (
	"strings"

	"github.com/go-richdoc/richdoc"
)

// A field is a {\field …} group: an instruction and the result Word last
// computed for it. The instruction is only ever read as data. Nothing in it is
// evaluated, fetched or run.
type field struct {
	mark int    // where in the paragraph's segments the result starts
	inst string // the \fldinst text
}

// endField finishes a field. A HYPERLINK becomes a link around its result; any
// other field leaves its result in place as plain text, which is what a reader
// that cannot compute a PAGE or a TOC shows.
func (r *reader) endField() {
	st := &r.st
	fr := st.fr
	f := st.fld
	mark := min(f.mark, len(fr.segs))
	url, title, ok := hyperlink(f.inst)
	if !ok || mark == len(fr.segs) {
		return
	}
	inl := r.inlines(fr.segs[mark:])
	fr.segs = fr.segs[:mark]
	r.node()
	fr.segs = append(fr.segs, seg{inl: richdoc.Link{URL: url, Title: title, Inlines: inl}})
}

// hyperlink reads a HYPERLINK field instruction (RTF 1.9.1 leaves field syntax
// to Word's own: `HYPERLINK "url" \l "fragment" \o "tooltip"`). It reports
// ok=false for any other field and for a target that is not safe to follow.
func hyperlink(inst string) (url, title string, ok bool) {
	toks := fieldTokens(inst)
	if len(toks) == 0 || !strings.EqualFold(toks[0], "HYPERLINK") {
		return "", "", false
	}
	var frag string
	for i := 1; i < len(toks); i++ {
		t := toks[i]
		if strings.HasPrefix(t, `\`) {
			// A switch with an argument takes the next token; \m and \n do not.
			switch strings.ToLower(t) {
			case `\l`, `\o`, `\t`:
				if i+1 < len(toks) {
					i++
					switch strings.ToLower(t) {
					case `\l`:
						frag = toks[i]
					case `\o`:
						title = toks[i]
					}
				}
			}
			continue
		}
		if url == "" {
			url = t
		}
	}
	if frag != "" {
		url += "#" + frag
	}
	if url == "" || !safeURL(url) {
		return "", "", false
	}
	return url, title, true
}

// fieldTokens splits an instruction on white space, keeping a quoted string
// whole and without its quotes.
func fieldTokens(s string) []string {
	var out []string
	for i := 0; i < len(s); {
		switch c := s[i]; {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == '"':
			j := strings.IndexByte(s[i+1:], '"')
			if j < 0 {
				out = append(out, s[i+1:])
				return out
			}
			out = append(out, s[i+1:i+1+j])
			i += j + 2
		default:
			j := i
			for j < len(s) && s[j] != ' ' && s[j] != '\t' && s[j] != '\r' && s[j] != '\n' {
				j++
			}
			out = append(out, s[i:j])
			i = j
		}
	}
	return out
}

// safeSchemes are the URL schemes a link may carry. A link in a converted
// document is followed by whoever reads it, so anything that runs code
// (javascript:, vbscript:, data:) or reaches a local or network file
// (file:, a UNC path) is dropped, and the text stays.
var safeSchemes = map[string]bool{
	"http": true, "https": true, "ftp": true, "ftps": true,
	"mailto": true, "tel": true, "news": true,
}

// safeURL accepts an allowed scheme, a fragment or a relative reference, and
// refuses control characters, backslashes (a Windows path or a UNC share) and
// a leading "//".
func safeURL(u string) bool {
	for i := 0; i < len(u); i++ {
		if u[i] < 0x20 || u[i] == 0x7f || u[i] == '\\' {
			return false
		}
	}
	if strings.HasPrefix(u, "//") {
		return false
	}
	// A scheme is what precedes the first colon, when nothing that ends a
	// scheme comes before it.
	for i := 0; i < len(u); i++ {
		switch u[i] {
		case ':':
			return safeSchemes[strings.ToLower(u[:i])]
		case '/', '?', '#':
			return true
		}
	}
	return true
}

// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package rtf

import (
	"bytes"
	"strings"

	"github.com/go-richdoc/richdoc"
)

// Parse reads an RTF document into a richdoc document.
//
// It never executes anything: an OLE \object is skipped (only its \result, the
// picture or text Word shows in its place, is read), a field is read for its
// text and, for HYPERLINK, its target, and nothing is fetched or evaluated.
//
// Parse returns an error wrapping [ErrLimit] when the file asks for more than
// lim allows, [ErrSyntax] for a \bin it cannot honour, and [ErrNotRTF] for
// input that is not RTF. Malformed RTF otherwise is read as far as it goes: an
// unclosed group is closed at the end of the input and an unknown control word
// is ignored, as the specification tells a reader to (RTF 1.9.1, "Conventions
// of an RTF Reader").
func Parse(src []byte, lim Limits) (*richdoc.Document, error) {
	lim = lim.orDefaults()
	if len(src) > lim.MaxInput {
		return nil, limitErr("input is %d bytes, the limit is %d", len(src), lim.MaxInput)
	}
	body := bytes.TrimLeft(bytes.TrimPrefix(src, []byte("\xef\xbb\xbf")), " \t\r\n")
	if !bytes.HasPrefix(body, []byte(`{\rtf`)) {
		return nil, ErrNotRTF
	}
	r := newReader(body, lim)
	r.run()
	if r.err != nil {
		return nil, r.err
	}
	return r.document(), nil
}

// A destKind says where the text under the current group goes.
type destKind uint8

const (
	dBody       destKind = iota // the document
	dSkip                       // nowhere: an unknown \* group, headers, shapes…
	dObject                     // an OLE object: ignored but for its \result
	dFontTbl                    // font table: only \fN and \fcharsetN are read
	dStyleSheet                 // style sheet: names and outline levels
	dInfo                       // \info: only \title and \author are read
	dTitle
	dAuthor
	dPict // picture data, as hex digits or \bin
	dFldInst
	dListTable
	dList
	dListLevel
	dListOverride
	dBookmark
	dPn // the old-style \pn paragraph numbering group
)

// ownership: a flag set on the group that STARTED a footnote, field, picture,
// bookmark or \pn group, so that closing it finishes the thing and closing a
// group nested inside does not. A child group never inherits these.
const (
	ownFootnote = 1 << iota
	ownField
	ownPict
	ownBookmark
	ownPn
)

// state is everything a group saves and restores.
type state struct {
	dest destKind
	own  uint8

	bold, italic, strike, hidden bool
	uc                           int // \ucN: how many fallback units follow a \uN
	font                         int

	// paragraph properties, reset by \pard
	ls, ilvl int
	style    int
	outline  int // \outlinelvlN, -1 when the paragraph does not say
	pn       int // 0 none, 1 bulleted, 2 numbered
	pnStart  int
	intbl    bool
	itap     int

	fr     *frame // where blocks and inlines go
	parent *frame // the frame a footnote group returns to
	fld    *field
	pict   *pict
}

type style struct {
	name    string
	outline int
}

type listDef struct {
	nfc   [9]int
	has   [9]bool
	start [9]int
}

type reader struct {
	src []byte
	pos int
	lim Limits
	err error

	st    state
	stack []state
	star  bool // a \* has just been read

	skip int  // fallback units still to skip after a \uN
	hi   rune // a high surrogate waiting for its low half
	pend []byte

	docCP    int
	defFont  int
	fontCP   map[int]int
	fontDef  int
	styles   map[int]*style
	curStyle int
	styleBuf string

	lists     map[int]*listDef
	overrides map[int]int
	counters  map[int]int
	curList   *listDef
	lvl       int
	ovrList   int
	ovrLS     int

	pnKind, pnStart int

	title, author string
	bkName        string

	nodes int
	text  int

	root *frame
	doc  []richdoc.Block
}

func newReader(src []byte, lim Limits) *reader {
	root := &frame{}
	r := &reader{
		src:       src,
		pos:       1,
		lim:       lim,
		docCP:     cpDefault,
		fontCP:    map[int]int{},
		styles:    map[int]*style{},
		curStyle:  -1,
		lists:     map[int]*listDef{},
		overrides: map[int]int{},
		counters:  map[int]int{},
		root:      root,
	}
	r.st = state{uc: 1, outline: -1, fr: root}
	return r
}

func (r *reader) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

// node counts one block or inline and refuses past the limit.
func (r *reader) node() {
	r.nodes++
	if r.nodes > r.lim.MaxNodes {
		r.fail(limitErr("more than %d nodes", r.lim.MaxNodes))
	}
}

func isSpecial(c byte) bool {
	return c == '{' || c == '}' || c == '\\' || c == '\r' || c == '\n'
}

// run is the reader's loop. The input is processed token by token with one
// explicit stack of saved states, so nothing here recurses.
func (r *reader) run() {
	for r.pos < len(r.src) && r.err == nil {
		switch c := r.src[r.pos]; c {
		case '{':
			r.flushPend()
			r.skip = 0
			r.star = false
			r.pos++
			r.push()
		case '}':
			r.flushPend()
			r.skip = 0
			r.star = false
			r.pos++
			if r.pop() {
				r.finish()
				return
			}
		case '\\':
			r.control()
		case '\r', '\n':
			r.pos++
		default:
			r.star = false
			r.textRun()
		}
	}
	if r.err == nil {
		r.flushPend()
		for !r.pop() { // close every group the input left open
		}
		r.finish()
	}
}

func (r *reader) finish() {
	if r.err == nil {
		r.doc = r.closeFrame()
	}
}

func (r *reader) push() {
	// The outermost group is on no stack, so the group about to open is at
	// depth len(stack)+2.
	if len(r.stack)+1 >= r.lim.MaxDepth {
		r.fail(limitErr("groups nested deeper than %d", r.lim.MaxDepth))
		return
	}
	r.stack = append(r.stack, r.st)
	r.st.own = 0
}

// pop closes a group and reports whether it was the outermost one.
func (r *reader) pop() bool {
	cur := r.st
	r.finalize()
	if len(r.stack) == 0 {
		return true
	}
	r.st = r.stack[len(r.stack)-1]
	r.stack = r.stack[:len(r.stack)-1]
	if cur.own&ownPn != 0 {
		r.st.pn, r.st.pnStart = r.pnKind, r.pnStart
	}
	return false
}

// finalize finishes what the closing group started.
func (r *reader) finalize() {
	st := &r.st
	switch {
	case st.own&ownFootnote != 0:
		blocks := r.closeFrame()
		if len(blocks) > 0 {
			r.node()
			st.parent.segs = append(st.parent.segs, seg{inl: richdoc.Footnote{Blocks: blocks}})
		}
	case st.own&ownField != 0:
		r.endField()
	case st.own&ownPict != 0:
		r.endPict()
	case st.own&ownBookmark != 0:
		if name := strings.TrimSpace(r.bkName); name != "" && name != "_GoBack" {
			r.node()
			st.fr.segs = append(st.fr.segs, seg{inl: richdoc.Anchor{ID: name}})
		}
	}
}

// textRun reads plain text up to the next special character.
func (r *reader) textRun() {
	start := r.pos
	for r.pos < len(r.src) && !isSpecial(r.src[r.pos]) {
		r.pos++
	}
	b := r.src[start:r.pos]
	if r.skip > 0 {
		n := min(r.skip, len(b))
		b = b[n:]
		r.skip -= n
	}
	r.feed(b)
}

// feed routes text bytes to where the current destination wants them.
func (r *reader) feed(b []byte) {
	switch r.st.dest {
	case dPict:
		r.hexData(b)
		return
	case dBody, dFldInst, dStyleSheet, dTitle, dAuthor, dBookmark:
	default:
		return
	}
	for i := 0; i < len(b); {
		if b[i] >= 0x80 {
			r.pend = append(r.pend, b[i])
			i++
			continue
		}
		j := i
		for j < len(b) && b[j] < 0x80 {
			j++
		}
		// Refuse before the run is copied into a string: a text of the whole
		// input must not cost a second copy of the input.
		if r.st.dest == dBody && r.text+(j-i) > r.lim.MaxText {
			r.fail(limitErr("more than %d bytes of text", r.lim.MaxText))
			return
		}
		r.flushPend()
		r.addString(string(b[i:j]))
		i = j
	}
}

// flushPend decodes the code-page bytes gathered so far. They are kept
// together because in a double-byte code page a character is two of them.
func (r *reader) flushPend() {
	if len(r.pend) == 0 {
		return
	}
	cp := r.docCP
	if c, ok := r.fontCP[r.st.font]; ok {
		cp = c
	}
	s := decode(cp, r.pend)
	r.pend = r.pend[:0]
	r.addString(s)
}

// addString puts decoded text where the current destination wants it.
func (r *reader) addString(s string) {
	if r.hi != 0 {
		r.hi = 0
		r.addString("�")
	}
	switch r.st.dest {
	case dBody:
		if r.st.hidden {
			return
		}
		r.text += len(s)
		if r.text > r.lim.MaxText {
			r.fail(limitErr("more than %d bytes of text", r.lim.MaxText))
			return
		}
		r.st.fr.addText(&r.st, s)
	case dFldInst:
		if f := r.st.fld; f != nil && len(f.inst) < maxInst {
			f.inst += s
		}
	case dStyleSheet:
		r.styleText(s)
	case dTitle:
		if len(r.title) < maxMeta {
			r.title += s
		}
	case dAuthor:
		if len(r.author) < maxMeta {
			r.author += s
		}
	case dBookmark:
		if len(r.bkName) < maxStyleName {
			r.bkName += s
		}
	}
}

// styleText reads the style sheet's text: each style's name, ended by ';'.
func (r *reader) styleText(s string) {
	for {
		i := strings.IndexByte(s, ';')
		if i < 0 {
			if len(r.styleBuf) < maxStyleName {
				r.styleBuf += s
			}
			return
		}
		r.styleBuf += s[:i]
		if st, ok := r.styles[r.curStyle]; ok {
			st.name = strings.ToLower(strings.TrimSpace(r.styleBuf))
		}
		r.styleBuf, r.curStyle = "", -1
		s = s[i+1:]
	}
}

// document assembles the result.
func (r *reader) document() *richdoc.Document {
	out := &richdoc.Document{Blocks: r.doc}
	meta := map[string]string{}
	if s := strings.TrimSpace(r.title); s != "" {
		meta["title"] = s
	}
	if s := strings.TrimSpace(r.author); s != "" {
		meta["author"] = s
	}
	if len(meta) > 0 {
		out.Meta = meta
	}
	return out
}

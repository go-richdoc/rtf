// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package rtf

import (
	"bytes"
	"slices"
	"strings"

	"github.com/go-richdoc/richdoc"
)

// A seg is a piece of the paragraph being read: a run of text with its
// character formatting, or an inline that is already built.
//
// A run's text is not a slice of its own: it is the range [start, end) of the
// frame's buffer, which holds the paragraph's text once, so a run costs no
// allocation and the paragraph's text becomes one string when it ends.
type seg struct {
	start, end           int
	inl                  richdoc.Inline
	bold, italic, strike bool
}

const (
	mergeNone uint8 = iota
	mergeFirst
	mergeCont
)

// A cellDef is what a table row's definition says about one cell.
type cellDef struct{ hmerge, vmerge uint8 }

type origin struct {
	row, cell int
	ok        bool
}

type openList struct {
	level   int
	ordered bool
	start   int
	items   []richdoc.ListItem
	cell    bool // opened inside a table cell
}

// A frame collects the blocks of one body of content: the document, or one
// footnote.
type frame struct {
	blocks []richdoc.Block
	segs   []seg
	buf    []byte // the text of the paragraph in progress; see seg
	lists  []*openList

	noMerge bool // the next text starts a segment of its own

	// the table being read
	rows      [][]richdoc.Cell
	header    []richdoc.Cell
	hasHeader bool
	cells     []richdoc.Cell // finished cells of the row in progress
	flags     []cellDef      // and what the row definition says about each
	defs      []cellDef
	pend      cellDef
	rowHdr    bool
	cellStart int // where in blocks the current cell's content starts
	origins   []origin
}

// addText appends text to the paragraph, joining it to the previous run when
// the formatting is the same.
//
// A new segment is a node, counted when it is made and not when the paragraph
// ends: a document of nothing but short differently-formatted runs and no
// paragraph break would otherwise grow without the limit seeing it.
func addText[T string | []byte](r *reader, s T) {
	st := &r.st
	f := st.fr
	merge := !f.noMerge
	f.noMerge = false
	start := len(f.buf)
	f.buf = append(grow(f.buf, len(s)), s...)
	if n := len(f.segs); n > 0 && merge {
		last := &f.segs[n-1]
		if last.inl == nil && last.end == start && last.bold == st.bold && last.italic == st.italic && last.strike == st.strike {
			last.end = len(f.buf)
			return
		}
	}
	// The text, and the wrapper inlines inlines will make for it.
	r.node()
	for _, wrapped := range [...]bool{st.strike, st.italic, st.bold} {
		if wrapped {
			r.node()
		}
	}
	f.segs = push(f.segs, seg{start: start, end: len(f.buf), bold: st.bold, italic: st.italic, strike: st.strike})
}

// push appends v to s, growing it as grow does.
func push[E any](s []E, v E) []E { return append(grow(s, 1), v) }

// grow returns s with room for n more elements, doubling the capacity when it
// has to make room. append grows a large slice by a quarter, and the copies it
// leaves behind add up to five times the final size: for a document of
// one-letter runs, most of what was allocated.
func grow[E any](s []E, n int) []E {
	if len(s)+n <= cap(s) {
		return s
	}
	grown := make([]E, len(s), max(2*len(s), len(s)+n))
	copy(grown, s)
	return grown
}

// inline adds an inline to the paragraph in progress.
func (r *reader) inline(in richdoc.Inline) {
	if r.st.dest == dBody && !r.st.hidden {
		r.node()
		r.st.fr.segs = push(r.st.fr.segs, seg{inl: in})
	}
}

// inlines builds richdoc inlines from a run of segments, each of which was
// counted when it was made, with the wrappers it will get here. Underline,
// size and colour are not in the model and are not carried.
func (r *reader) inlines(fr *frame, segs []seg) []richdoc.Inline {
	// One string holds the text of each contiguous group of runs, and each Text
	// is a piece of it. A group ends at an inline, or where the buffer skips the
	// bytes of a field already built: copying the whole span from the first run
	// to the last would copy those bytes again at every level of nesting.
	out := make([]richdoc.Inline, 0, len(segs))
	var all string
	lo, prev := 0, -1 // the group's offset in fr.buf, and where the last run ended
	for i, s := range segs {
		if s.inl != nil {
			out = append(out, s.inl)
			prev = -1
			continue
		}
		if s.start != prev {
			lo = s.start
			hi := s.end
			for j := i + 1; j < len(segs) && segs[j].inl == nil && segs[j].start == hi; j++ {
				hi = segs[j].end
			}
			all = string(fr.buf[lo:hi])
		}
		prev = s.end
		var in richdoc.Inline = richdoc.Text{Value: all[s.start-lo : s.end-lo]}
		if s.strike {
			in = richdoc.Strikethrough{Inlines: []richdoc.Inline{in}}
		}
		if s.italic {
			in = richdoc.Emph{Inlines: []richdoc.Inline{in}}
		}
		if s.bold {
			in = richdoc.Strong{Inlines: []richdoc.Inline{in}}
		}
		out = append(out, in)
	}
	return out
}

// blank reports whether the paragraph holds nothing but white space.
func blank(fr *frame) bool {
	for _, s := range fr.segs {
		if s.inl != nil || len(bytes.TrimSpace(fr.buf[s.start:s.end])) > 0 {
			return false
		}
	}
	return true
}

// endPara ends the paragraph in progress. force is for a paragraph that is
// known to be inside a table cell whether or not \intbl said so.
func (r *reader) endPara(force bool) {
	st := &r.st
	fr := st.fr
	if blank(fr) {
		fr.segs, fr.buf = fr.segs[:0], fr.buf[:0]
		return
	}
	inl := r.inlines(fr, fr.segs)
	fr.segs, fr.buf = fr.segs[:0], fr.buf[:0]
	if !force && !st.intbl {
		r.flushTable(fr)
	}
	r.node()
	if (force || st.intbl) && len(fr.lists) > 0 && !fr.lists[0].cell {
		// A list still open when a table starts belongs before the table,
		// not in its first cell.
		r.closeLists(fr)
		fr.cellStart = len(fr.blocks)
	}
	if lv := r.headingLevel(); lv > 0 {
		r.closeLists(fr)
		fr.blocks = push[richdoc.Block](fr.blocks, richdoc.Heading{Level: lv, Inlines: inl})
	} else if ok, ordered, start, level := r.listOf(); ok {
		r.addListItem(fr, richdoc.Paragraph{Inlines: inl, Classes: st.align.classes()}, ordered, start, level)
	} else {
		r.closeLists(fr)
		fr.blocks = push[richdoc.Block](fr.blocks, richdoc.Paragraph{Inlines: inl, Classes: st.align.classes()})
	}
	if !force && !st.intbl {
		fr.cellStart = len(fr.blocks)
	}
}

// The paragraph alignments RTF has beyond the default (\ql). richdoc has no
// paragraph alignment, only the "Classes" hint, so each is carried as one.
type alignment uint8

const (
	alignNone alignment = iota
	alignCenter
	alignRight
	alignJustify
)

// classes is the Classes hint for an alignment, nil for the default.
func (a alignment) classes() []string {
	switch a {
	case alignCenter:
		return []string{"align-center"}
	case alignRight:
		return []string{"align-right"}
	case alignJustify:
		return []string{"align-justify"}
	}
	return nil
}

// headingLevel is the heading level of the paragraph, 0 for body text. A
// paragraph is a heading when it says so (\outlinelvlN) or its style does,
// whether by outline level or by the name Word gives its built-in styles.
func (r *reader) headingLevel() int {
	st := &r.st
	o := st.outline
	if o < 0 {
		if s, ok := r.styles[st.style]; ok {
			o = s.outline
			if o < 0 {
				if n, ok := strings.CutPrefix(s.name, "heading "); ok && len(n) == 1 && n[0] >= '1' && n[0] <= '9' {
					o = int(n[0] - '1')
				}
			}
		}
	}
	if o < 0 || o > 8 {
		return 0
	}
	return min(o+1, 6)
}

// listOf says whether the paragraph is a list item and of what kind.
func (r *reader) listOf() (ok, ordered bool, start, level int) {
	st := &r.st
	if st.ls > 0 {
		level = st.ilvl
		start = 1
		if def := r.lists[r.overrides[st.ls]]; def != nil && def.has[level] {
			nfc := def.nfc[level]
			ordered = nfc != 23 && nfc != 255
			// \levelstartat0 is a list that counts from 0, and stays 0 here: a
			// list resumed after k items starts at k whatever it began at.
			// richdoc.List.Start reads 0 as "unset", so a list that begins at 0
			// is shown from 1: the one case the model cannot say.
			start = def.start[level]
		}
		if ordered && level == 0 {
			start += r.counters[st.ls]
		}
		return true, ordered, start, level
	}
	if st.pn != 0 {
		return true, st.pn == 2, max(1, st.pnStart), 0
	}
	return false, false, 0, 0
}

// addListItem puts a paragraph into the list it belongs to, opening and
// closing lists so that the nesting matches its level.
func (r *reader) addListItem(fr *frame, para richdoc.Block, ordered bool, start, level int) {
	for n := len(fr.lists); n > 0 && fr.lists[n-1].level > level; n = len(fr.lists) {
		r.popList(fr)
	}
	if n := len(fr.lists); n > 0 && fr.lists[n-1].level == level && fr.lists[n-1].ordered != ordered {
		r.popList(fr)
	}
	if n := len(fr.lists); n == 0 || fr.lists[n-1].level < level {
		// Every open list holds at least the item that opened it, so a list
		// nested here always has an item to go into.
		fr.lists = append(fr.lists, &openList{level: level, ordered: ordered, start: start, cell: r.st.intbl || len(fr.cells) > 0})
	}
	top := fr.lists[len(fr.lists)-1]
	r.node()
	top.items = push(top.items, richdoc.ListItem{Blocks: []richdoc.Block{para}})
	if ordered && level == 0 {
		r.counters[r.st.ls]++
	}
}

// popList closes the innermost open list into the item above it, or into the
// frame's blocks when there is none.
func (r *reader) popList(fr *frame) {
	top := fr.lists[len(fr.lists)-1]
	fr.lists = fr.lists[:len(fr.lists)-1]
	r.node()
	l := richdoc.List{Ordered: top.ordered, Tight: true, Items: top.items}
	if top.ordered {
		l.Start = top.start
	}
	if n := len(fr.lists); n > 0 {
		p := fr.lists[n-1]
		item := &p.items[len(p.items)-1]
		item.Blocks = append(item.Blocks, l)
		return
	}
	fr.blocks = push[richdoc.Block](fr.blocks, l)
}

func (r *reader) closeLists(fr *frame) {
	for len(fr.lists) > 0 {
		r.popList(fr)
	}
}

// pageBreak is \page. richdoc has no page break; the nearest thing the model
// carries is a thematic break, so that is what a reader gets.
//
// Inside a table cell it is nothing at all: the cell is one cell, and the text
// on either side of the break stays in it (as textutil and LibreOffice do). A
// break in the middle of a table cannot be carried by a model whose table
// holds cells.
func (r *reader) pageBreak() {
	fr := r.st.fr
	if r.st.intbl {
		return
	}
	r.endPara(false)
	r.flushTable(fr)
	r.closeLists(fr)
	r.node()
	fr.blocks = push[richdoc.Block](fr.blocks, richdoc.ThematicBreak{})
	fr.cellStart = len(fr.blocks)
}

// footnote starts a footnote group: its own frame, and (RTF 1.9.1, the
// "Destinations" section) none of the surrounding formatting.
func (r *reader) footnote() {
	st := &r.st
	st.parent = st.fr
	st.fr = &frame{}
	st.own |= ownFootnote
	st.bold, st.italic, st.strike, st.hidden = false, false, false, false
	st.ls, st.ilvl, st.style, st.outline = 0, 0, 0, -1
	st.pn, st.pnStart, st.intbl, st.itap = 0, 0, false, 0
	st.align = alignNone
}

// closeFrame finishes the frame the current group is writing and returns its
// blocks.
func (r *reader) closeFrame() []richdoc.Block {
	fr := r.st.fr
	if len(fr.cells) > 0 {
		r.endRow()
	}
	r.endPara(false)
	r.flushTable(fr)
	r.closeLists(fr)
	return fr.blocks
}

// --- tables ---

func (f *frame) startRow() {
	f.defs, f.pend, f.rowHdr = nil, cellDef{}, false
}

func (r *reader) addCellDef(f *frame) {
	r.node()
	f.defs = push(f.defs, f.pend)
	f.pend = cellDef{}
}

func (f *frame) defAt(i int) cellDef {
	if i < len(f.defs) {
		return f.defs[i]
	}
	return cellDef{}
}

// endCell ends a table cell: what has been read since the last one is its
// content.
func (r *reader) endCell() {
	fr := r.st.fr
	r.endPara(true)
	r.closeLists(fr)
	r.node()
	start := min(fr.cellStart, len(fr.blocks))
	content := slices.Clone(fr.blocks[start:])
	fr.blocks = fr.blocks[:start]
	fr.flags = push(fr.flags, fr.defAt(len(fr.cells)))
	fr.cells = push(fr.cells, makeCell(content))
	fr.cellStart = len(fr.blocks)
}

// makeCell builds a cell. A cell with one paragraph sets Inlines alone; one
// with anything more sets Blocks and the flattened Inlines too, as the model
// asks (see richdoc.Cell).
func makeCell(blocks []richdoc.Block) richdoc.Cell {
	switch len(blocks) {
	case 0:
		return richdoc.Cell{}
	case 1:
		if p, ok := blocks[0].(richdoc.Paragraph); ok {
			return richdoc.Cell{Inlines: p.Inlines}
		}
	}
	return richdoc.Cell{Blocks: blocks, Inlines: flatten(blocks, nil)}
}

// flatten is the cell's content as one line of inlines, a line break between
// its parts.
func flatten(blocks []richdoc.Block, out []richdoc.Inline) []richdoc.Inline {
	for _, b := range blocks {
		var in []richdoc.Inline
		switch b := b.(type) {
		case richdoc.Paragraph:
			in = b.Inlines
		case richdoc.Heading:
			in = b.Inlines
		case richdoc.List:
			for _, it := range b.Items {
				out = flatten(it.Blocks, out)
			}
			continue
		}
		if len(out) > 0 {
			out = append(out, richdoc.LineBreak{})
		}
		out = append(out, in...)
	}
	return out
}

// endRow ends a table row, applying the merges its cells asked for.
func (r *reader) endRow() {
	fr := r.st.fr
	if len(fr.blocks) > fr.cellStart || !blank(fr) {
		r.endCell()
	}
	if len(fr.cells) == 0 {
		fr.defs = nil
		return
	}
	header := fr.rowHdr && !fr.hasHeader && len(fr.rows) == 0
	// The row is built in the cells' own array: a cell is written at or before
	// the place it was read from, and fr.cells is dropped below.
	row := fr.cells[:0]
	fr.origins = append(fr.origins, make([]origin, max(0, len(fr.cells)-len(fr.origins)))...)
	for i, c := range fr.cells {
		fl := fr.flags[i]
		if fl.hmerge == mergeCont && len(row) > 0 {
			last := &row[len(row)-1]
			last.ColSpan = max(last.ColSpan, 1) + 1
			fr.origins[i] = origin{}
			continue
		}
		if o := fr.origins[i]; fl.vmerge == mergeCont && o.ok {
			cell := &fr.rows[o.row][o.cell]
			cell.RowSpan = max(cell.RowSpan, 1) + 1
			continue
		}
		row = append(row, c)
		fr.origins[i] = origin{}
		if fl.vmerge == mergeFirst && !header {
			fr.origins[i] = origin{row: len(fr.rows), cell: len(row) - 1, ok: true}
		}
	}
	if header {
		fr.header, fr.hasHeader = row, true
	} else {
		fr.rows = push(fr.rows, row)
	}
	fr.cells, fr.flags, fr.defs, fr.rowHdr = nil, nil, nil, false
}

// flushTable emits the table read so far, if there is one.
func (r *reader) flushTable(fr *frame) {
	if len(fr.rows) == 0 && !fr.hasHeader {
		return
	}
	r.closeLists(fr)
	r.node()
	fr.blocks = append(fr.blocks, richdoc.Table{Header: fr.header, Rows: fr.rows})
	fr.rows, fr.header, fr.hasHeader, fr.origins = nil, nil, false, nil
	fr.cellStart = len(fr.blocks)
}

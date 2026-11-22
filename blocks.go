// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package rtf

import (
	"slices"
	"strings"

	"github.com/go-richdoc/richdoc"
)

// A seg is a piece of the paragraph being read: a run of text with its
// character formatting, or an inline that is already built.
type seg struct {
	text                 []byte
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
func (f *frame) addText(st *state, s string) {
	merge := !f.noMerge
	f.noMerge = false
	if n := len(f.segs); n > 0 && merge {
		last := &f.segs[n-1]
		if last.inl == nil && last.bold == st.bold && last.italic == st.italic && last.strike == st.strike {
			last.text = append(last.text, s...)
			return
		}
	}
	f.segs = append(f.segs, seg{text: []byte(s), bold: st.bold, italic: st.italic, strike: st.strike})
}

// inline adds an inline to the paragraph in progress.
func (r *reader) inline(in richdoc.Inline) {
	if r.st.dest == dBody && !r.st.hidden {
		r.st.fr.segs = append(r.st.fr.segs, seg{inl: in})
	}
}

// inlines builds richdoc inlines from a run of segments. Underline, size and
// colour are not in the model and are not carried.
func (r *reader) inlines(segs []seg) []richdoc.Inline {
	out := make([]richdoc.Inline, 0, len(segs))
	for _, s := range segs {
		r.node()
		if s.inl != nil {
			out = append(out, s.inl)
			continue
		}
		var in richdoc.Inline = richdoc.Text{Value: string(s.text)}
		if s.strike {
			r.node()
			in = richdoc.Strikethrough{Inlines: []richdoc.Inline{in}}
		}
		if s.italic {
			r.node()
			in = richdoc.Emph{Inlines: []richdoc.Inline{in}}
		}
		if s.bold {
			r.node()
			in = richdoc.Strong{Inlines: []richdoc.Inline{in}}
		}
		out = append(out, in)
	}
	return out
}

// blank reports whether the paragraph holds nothing but white space.
func blank(segs []seg) bool {
	for _, s := range segs {
		if s.inl != nil || len(strings.TrimSpace(string(s.text))) > 0 {
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
	if blank(fr.segs) {
		fr.segs = fr.segs[:0]
		return
	}
	inl := r.inlines(fr.segs)
	fr.segs = fr.segs[:0]
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
		fr.blocks = append(fr.blocks, richdoc.Heading{Level: lv, Inlines: inl})
	} else if ok, ordered, start, level := r.listOf(); ok {
		r.addListItem(fr, richdoc.Paragraph{Inlines: inl}, ordered, start, level)
	} else {
		r.closeLists(fr)
		fr.blocks = append(fr.blocks, richdoc.Paragraph{Inlines: inl})
	}
	if !force && !st.intbl {
		fr.cellStart = len(fr.blocks)
	}
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
			start = max(1, def.start[level])
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
	top.items = append(top.items, richdoc.ListItem{Blocks: []richdoc.Block{para}})
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
	fr.blocks = append(fr.blocks, l)
}

func (r *reader) closeLists(fr *frame) {
	for len(fr.lists) > 0 {
		r.popList(fr)
	}
}

// pageBreak is \page. richdoc has no page break; the nearest thing the model
// carries is a thematic break, so that is what a reader gets.
func (r *reader) pageBreak() {
	fr := r.st.fr
	r.endPara(false)
	r.flushTable(fr)
	r.closeLists(fr)
	r.node()
	fr.blocks = append(fr.blocks, richdoc.ThematicBreak{})
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
	f.defs = append(f.defs, f.pend)
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
	fr.flags = append(fr.flags, fr.defAt(len(fr.cells)))
	fr.cells = append(fr.cells, makeCell(content))
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
	if len(fr.blocks) > fr.cellStart || !blank(fr.segs) {
		r.endCell()
	}
	if len(fr.cells) == 0 {
		fr.defs = nil
		return
	}
	header := fr.rowHdr && !fr.hasHeader && len(fr.rows) == 0
	row := make([]richdoc.Cell, 0, len(fr.cells))
	for len(fr.origins) < len(fr.cells) {
		fr.origins = append(fr.origins, origin{})
	}
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
		fr.rows = append(fr.rows, row)
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

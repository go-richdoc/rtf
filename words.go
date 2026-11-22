// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package rtf

import (
	"unicode/utf16"

	"github.com/go-richdoc/richdoc"
)

// canEnter reports whether a destination word here starts something: not
// inside a skipped group, and not inside an object, whose only readable part
// is its \result.
func (r *reader) canEnter() bool {
	return r.st.dest != dSkip && r.st.dest != dObject
}

func clamp(v, lo, hi int) int { return max(lo, min(hi, v)) }

// toggle is a control word with an on/off parameter: bare or non-zero is on,
// zero is off (RTF 1.9.1, "Control Word").
func toggle(p int, has bool) bool { return !has || p != 0 }

// word acts on one control word.
//
// The groups of words are: character and paragraph properties, characters
// that a word stands for, destinations, and the table and list vocabularies.
// A word this does not know is ignored; if it follows \* its whole group is
// dropped, which is what the \* is for.
func (r *reader) word(name string, p int, has bool) {
	star := r.star
	r.star = false
	st := &r.st
	switch st.dest {
	case dSkip:
		return
	case dObject:
		if name == "result" {
			st.dest = dBody
		}
		return
	}
	if r.docWord(name, p, has, star) {
		return
	}
	if star {
		st.dest = dSkip // an ignorable destination this reader does not know
	}
}

// docWord handles every word the reader knows and reports whether it did.
func (r *reader) docWord(name string, p int, has, star bool) bool {
	st := &r.st
	switch name {
	// --- character properties ---
	case "b":
		st.bold = toggle(p, has)
	case "i":
		st.italic = toggle(p, has)
	case "strike", "striked":
		st.strike = toggle(p, has)
	case "v":
		st.hidden = toggle(p, has)
	case "plain":
		st.bold, st.italic, st.strike, st.hidden = false, false, false, false
		st.font = r.defFont
	case "f":
		if st.dest == dFontTbl {
			r.fontDef = p
		} else {
			st.font = p
		}
	case "fcharset":
		if st.dest == dFontTbl {
			if cp, ok := charsetCP[p]; ok {
				r.fontCP[r.fontDef] = cp
			}
		}
	case "deff":
		r.defFont = p
		st.font = p
	case "ansicpg":
		r.docCP = clamp(p, 0, 1<<20)
	case "ansi":
		r.docCP = cpDefault
	case "mac":
		r.docCP = 10000
	case "pc":
		r.docCP = 437
	case "pca":
		r.docCP = 850
	case "uc":
		st.uc = clamp(p, 0, 255)
	case "u":
		if has {
			r.unicode(p)
		}

	// --- paragraph properties ---
	case "pard":
		st.ls, st.ilvl, st.style, st.outline = 0, 0, 0, -1
		st.pn, st.pnStart, st.intbl, st.itap = 0, 0, false, 0
		st.align = alignNone
	case "ql":
		st.align = alignNone
	case "qc":
		st.align = alignCenter
	case "qr":
		st.align = alignRight
	case "qj":
		st.align = alignJustify
	case "ls":
		if st.dest == dListOverride {
			r.ovrLS = p
			r.mapOverride()
		} else {
			st.ls = p
		}
	case "ilvl":
		st.ilvl = clamp(p, 0, 8)
	case "s":
		if st.dest == dStyleSheet {
			r.curStyle = p
			if _, ok := r.styles[p]; !ok {
				r.node()
				r.styles[p] = &style{outline: -1}
			}
		} else {
			st.style = p
		}
	case "outlinelvl":
		if st.dest == dStyleSheet {
			if s, ok := r.styles[r.curStyle]; ok {
				s.outline = p
			}
		} else {
			st.outline = p
		}
	case "intbl":
		st.intbl = true
		st.itap = max(st.itap, 1)
	case "itap":
		st.itap = clamp(p, 0, 1<<10)
		st.intbl = st.itap > 0

	// --- characters ---
	case "par", "sect":
		if st.dest == dBody {
			r.endPara(false)
		}
	case "line":
		r.inline(richdoc.LineBreak{})
	case "page":
		if st.dest == dBody {
			r.pageBreak()
		}
	case "tab":
		r.addString("\t")
	case "emdash":
		r.addString("—")
	case "endash":
		r.addString("–")
	case "bullet":
		r.addString("•")
	case "lquote":
		r.addString("‘")
	case "rquote":
		r.addString("’")
	case "ldblquote":
		r.addString("“")
	case "rdblquote":
		r.addString("”")
	case "enspace":
		r.addString(string(rune(0x2002)))
	case "emspace":
		r.addString(string(rune(0x2003)))
	case "qmspace":
		r.addString(string(rune(0x2005)))
	case "zwj":
		r.addString(string(rune(0x200D)))
	case "zwnj":
		r.addString(string(rune(0x200C)))

	// --- tables ---
	case "trowd":
		if st.itap <= 1 {
			r.st.fr.startRow()
		}
	case "trhdr":
		st.fr.rowHdr = true
	case "clmgf":
		st.fr.pend.hmerge = mergeFirst
	case "clmrg":
		st.fr.pend.hmerge = mergeCont
	case "clvmgf":
		st.fr.pend.vmerge = mergeFirst
	case "clvmrg":
		st.fr.pend.vmerge = mergeCont
	case "cellx":
		r.addCellDef(st.fr)
	case "cell":
		if st.dest == dBody {
			if st.itap > 1 {
				r.endPara(true)
			} else {
				r.endCell()
			}
		}
	case "nestcell":
		if st.dest == dBody {
			r.endPara(true)
		}
	case "row":
		if st.dest == dBody {
			r.endRow()
			// What follows a row is not in the table, whether or not a \pard
			// says so: writers that omit it still mean the table is over.
			if st.itap <= 1 {
				st.intbl, st.itap = false, 0
			}
		}

	// --- destinations ---
	case "fonttbl":
		return r.enter(dFontTbl)
	case "stylesheet":
		return r.enter(dStyleSheet)
	case "info":
		return r.enter(dInfo)
	case "title":
		if st.dest == dInfo {
			st.dest = dTitle
		}
	case "author":
		if st.dest == dInfo {
			st.dest = dAuthor
		}
	case "pict":
		if r.canEnter() {
			st.dest = dPict
			st.pict = &pict{}
			st.own |= ownPict
		}
	case "pngblip":
		if st.dest == dPict {
			st.pict.kind = picPNG
		}
	case "jpegblip":
		if st.dest == dPict {
			st.pict.kind = picJPEG
		}
	case "picwgoal":
		if st.dest == dPict {
			st.pict.wGoal = clamp(p, 0, 1<<24)
		}
	case "pichgoal":
		if st.dest == dPict {
			st.pict.hGoal = clamp(p, 0, 1<<24)
		}
	case "picscalex":
		if st.dest == dPict {
			st.pict.sx = clamp(p, 0, 1<<14)
		}
	case "picscaley":
		if st.dest == dPict {
			st.pict.sy = clamp(p, 0, 1<<14)
		}
	case "object":
		return r.enter(dObject)
	case "field":
		if st.dest == dBody {
			// The result must start a segment of its own, or it would be
			// merged into the text before the field and could not be wrapped.
			st.fr.noMerge = true
			st.fld = &field{mark: len(st.fr.segs)}
			st.own |= ownField
		}
	case "fldinst":
		// An instruction with no field around it is not document text.
		if st.dest == dBody {
			st.dest = dSkip
			if st.fld != nil {
				st.dest = dFldInst
			}
		}
	case "fldrslt", "shppict", "ud":
		// read in place: the group's text is document text
	case "footnote":
		if st.dest == dBody {
			r.footnote()
		}
	case "bkmkstart":
		if st.dest == dBody {
			st.dest = dBookmark
			r.bkName = r.bkName[:0]
			st.own |= ownBookmark
		}
	case "pn":
		if star && st.dest == dBody {
			st.dest = dPn
			st.own |= ownPn
			r.pnKind, r.pnStart = 0, 0
		}
	case "pnlvlblt":
		r.pnKind = 1
	case "pnlvlbody", "pndec", "pnucrm", "pnlcrm", "pnucltr", "pnlcltr":
		r.pnKind = 2
	case "pnstart":
		r.pnStart = clamp(p, 0, 1<<20)
	case "colortbl", "header", "headerl", "headerr", "headerf",
		"footer", "footerl", "footerr", "footerf", "annotation",
		"atnauthor", "atnid", "atndate", "atnref", "atntime", "atnicn",
		"pntext", "listtext", "upr", "shp", "nonshppict", "revtbl",
		"xe", "tc", "tcn", "txe", "rxe", "ixe", "defchp", "defpap",
		"ftnsep", "ftnsepc", "aftnsep", "aftnsepc", "ftncn", "aftncn",
		"leveltext", "levelnumbers", "listname", "mmath", "protusertbl",
		"xmlnstbl", "htmltag", "mhtmltag", "pgptbl", "fchars", "lchars",
		"themedata", "bkmkend":
		return r.enter(dSkip)

	// --- lists ---
	case "listtable":
		return r.enter(dListTable)
	case "listoverridetable":
		return r.enter(dListTable)
	case "list":
		if st.dest == dListTable {
			st.dest = dList
			r.curList = &listDef{}
			r.lvl = -1
			r.node()
		}
	case "listid":
		switch st.dest {
		case dList:
			r.lists[p] = r.curList
		case dListOverride:
			r.ovrList = p
			r.mapOverride()
		}
	case "listoverride":
		if st.dest == dListTable {
			st.dest = dListOverride
			r.ovrList, r.ovrLS = 0, 0
		}
	case "listlevel":
		if st.dest == dList {
			st.dest = dListLevel
			r.lvl++
			if r.lvl < 9 && r.curList != nil {
				r.curList.start[r.lvl] = 1 // \levelstartat's default
			}
		}
	case "levelnfc", "levelnfcn":
		if st.dest == dListLevel && r.lvl >= 0 && r.lvl < 9 && r.curList != nil {
			r.curList.nfc[r.lvl], r.curList.has[r.lvl] = p, true
		}
	case "levelstartat":
		if st.dest == dListLevel && r.lvl >= 0 && r.lvl < 9 && r.curList != nil {
			r.curList.start[r.lvl] = clamp(p, 0, 1<<20)
		}
	default:
		return false
	}
	return true
}

// enter makes the current group a destination of kind k. It reports true so
// callers can return it: a known destination is never "unknown", so \* before
// it does not drop it.
func (r *reader) enter(k destKind) bool {
	if r.canEnter() {
		r.st.dest = k
	}
	return true
}

// mapOverride records \lsN -> \listidN once both have been read.
func (r *reader) mapOverride() {
	if r.ovrLS > 0 && r.ovrList != 0 {
		r.overrides[r.ovrLS] = r.ovrList
	}
}

// unicode handles \uN: a 16-bit signed code unit, followed by uc fallback
// units to skip (RTF 1.9.1, \uN and \ucN).
func (r *reader) unicode(n int) {
	r.skip = r.st.uc
	if n < 0 {
		n += 0x10000
	}
	v := rune(n & 0xffff)
	switch {
	case v >= 0xD800 && v < 0xDC00:
		if r.hi != 0 {
			r.hi = 0
			r.addString("�")
		}
		r.hi = v
	case v >= 0xDC00 && v < 0xE000:
		// DecodeRune answers U+FFFD for a low half with no high half before it.
		pair := utf16.DecodeRune(r.hi, v)
		r.hi = 0
		r.addString(string(pair))
	default:
		r.addString(string(v))
	}
}

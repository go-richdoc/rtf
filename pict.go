// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package rtf

import (
	"bytes"
	"encoding/base64"
	"image/jpeg"
	"image/png"
	"strconv"

	"github.com/go-richdoc/richdoc"
)

const (
	picNone = iota
	picPNG
	picJPEG
)

// A pict is a {\pict …} group being read. Only \pngblip and \jpegblip
// pictures are kept: a metafile (\emfblip, \wmetafile) or a device bitmap
// would need a renderer this model has no node for.
type pict struct {
	kind   int
	data   []byte
	nib    int // a first hex digit waiting for its second, or -1 when none is
	hasNib bool

	wGoal, hGoal int // \picwgoal, \pichgoal, twips
	sx, sy       int // \picscalex, \picscaley, percent; 0 when not given
}

// add appends decoded picture bytes, refusing past the limit BEFORE the
// append, so that a picture cannot grow past MaxPict by one chunk.
func (p *pict) add(r *reader, b []byte) {
	if len(p.data)+len(b) > r.lim.MaxPict {
		r.fail(limitErr("a picture of more than %d bytes", r.lim.MaxPict))
		return
	}
	p.data = append(p.data, b...)
}

// hexData reads picture data written as hex digits. Anything that is not a
// hex digit (a line break, a space) is ignored, as the specification allows
// RTF writers to break the data anywhere.
func (r *reader) hexData(b []byte) {
	p := r.st.pict
	for _, c := range b {
		v := hexVal(c)
		if v < 0 {
			continue
		}
		if !p.hasNib {
			p.nib, p.hasNib = v, true
			continue
		}
		p.hasNib = false
		if len(p.data)+1 > r.lim.MaxPict {
			r.fail(limitErr("a picture of more than %d bytes", r.lim.MaxPict))
			return
		}
		p.data = append(p.data, byte(p.nib<<4|v))
	}
}

var (
	pngMagic  = []byte("\x89PNG\r\n\x1a\n")
	jpegMagic = []byte("\xff\xd8\xff")
)

// endPict turns a finished picture group into an image, as a data URL, the way
// the other readers carry theirs. The data is checked against the format the
// control word claimed: a \pngblip that is not a PNG is dropped, not passed
// downstream under a type it does not have.
//
// The bytes are bounded by the input (hex is two characters per byte, \bin one),
// so the data URL costs at most 4/3 of the source and needs no total of its own.
func (r *reader) endPict() {
	st := &r.st
	p := st.pict
	if st.hidden || p == nil {
		return
	}
	// The header is read, not only the signature: png.DecodeConfig checks the
	// IHDR chunk (its length, CRC, a non-zero size, a colour type and depth
	// that go together) and jpeg.DecodeConfig the frame header. Neither decodes
	// pixels, so a picture that declares a vast size is still the caller's to
	// bound when it decodes; what is refused here is bytes that are not an
	// image of the claimed kind.
	var mime string
	var err error
	switch {
	case p.kind == picPNG && bytes.HasPrefix(p.data, pngMagic):
		mime = "image/png"
		_, err = png.DecodeConfig(bytes.NewReader(p.data))
	case p.kind == picJPEG && bytes.HasPrefix(p.data, jpegMagic):
		mime = "image/jpeg"
		_, err = jpeg.DecodeConfig(bytes.NewReader(p.data))
	default:
		return
	}
	if err != nil {
		return
	}
	img := richdoc.Image{URL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(p.data)}
	sx, sy := p.sx, p.sy
	if p.wGoal > 0 && p.hGoal > 0 {
		img.Width = points(p.wGoal, sx)
		img.Height = points(p.hGoal, sy)
	} else if sx > 0 && sx == sy && sx != 100 {
		img.Scale = sx
	}
	r.node()
	st.fr.segs = append(st.fr.segs, seg{inl: img})
}

// points is a size given in twips, scaled by a percentage when there is one,
// as the "pt" length richdoc.Image.Width takes. 1 pt is 20 twips.
func points(twips, scale int) string {
	if scale > 0 {
		twips = int(int64(twips) * int64(scale) / 100)
	}
	return strconv.Itoa((twips+10)/20) + "pt"
}

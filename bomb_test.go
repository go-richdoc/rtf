// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package rtf

import (
	"errors"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/go-richdoc/richdoc/bombtest"
)

// Each bomb is an input far past a small limit, and each must be REFUSED while
// asking the allocator for little: the refusal has to come before the work, not
// after the document has been built. The inputs are made before bombtest.Run
// starts counting, so what is measured is the reader and nothing else.

const (
	heldCap = 256 << 20 // the process may hold this much while a bomb runs
	mib     = 1 << 20
)

func bomb(t *testing.T, why string, asks uint64, want error, in []byte, lim Limits) {
	t.Helper()
	res := bombtest.Run(t, bombtest.Bound{HeldAtMost: heldCap, AsksAtMost: asks, Why: why}, func() error {
		_, err := Parse(in, lim)
		return err
	})
	if !errors.Is(res.Err, want) {
		t.Errorf("%s: got %v, want an error wrapping %v", why, res.Err, want)
	}
	t.Log(res)
}

func TestBombs(t *testing.T) {
	head := `{\rtf1 `
	t.Run("group depth", func(t *testing.T) {
		// 4 MB of open braces against the default depth cap.
		in := []byte(head + strings.Repeat("{", 4*mib))
		bomb(t, "unbounded group nesting is refused at the depth cap", 128<<10, ErrLimit, in, Limits{})
	})
	t.Run("bin larger than the input", func(t *testing.T) {
		in := []byte(head + `\pict\pngblip\bin2147483647 x}`)
		bomb(t, "a ten-digit \\bin cannot name memory the input does not hold", 4<<10, ErrSyntax, in, Limits{})
	})
	t.Run("bin saturated", func(t *testing.T) {
		in := []byte(head + `\bin` + strings.Repeat("9", 4096) + ` x}`)
		bomb(t, "a 4096-digit \\bin is saturated, not overflowed", 8<<10, ErrSyntax, in, Limits{})
	})
	t.Run("bin larger than MaxBin", func(t *testing.T) {
		in := []byte(head + `\bin` + strconv.Itoa(8*mib) + ` ` + strings.Repeat("z", 8*mib) + `}`)
		bomb(t, "a \\bin past MaxBin is refused without being copied", 8<<10, ErrLimit, in, Limits{MaxBin: mib})
	})
	t.Run("hex picture", func(t *testing.T) {
		in := []byte(head + `{\pict\pngblip ` + strings.Repeat("ab", 8*mib) + `}}`)
		bomb(t, "hex picture data stops at MaxPict, not at the end of the stream", 5*(64<<10), ErrLimit, in, Limits{MaxPict: 64 << 10})
	})
	t.Run("bin pictures", func(t *testing.T) {
		in := []byte(head + `{\pict\pngblip` + strings.Repeat(`\bin1024 `+strings.Repeat("z", 1024), 4096) + `}}`)
		bomb(t, "picture chunks add up to MaxPict, not past it", 5*(64<<10), ErrLimit, in, Limits{MaxPict: 64 << 10})
	})
	t.Run("paragraph flood", func(t *testing.T) {
		in := []byte(head + strings.Repeat(`a\par `, mib))
		bomb(t, "paragraph nodes stop at MaxNodes", 8*mib, ErrLimit, in, Limits{MaxNodes: 1 << 16})
	})
	t.Run("cell flood", func(t *testing.T) {
		in := []byte(head + `\trowd` + strings.Repeat(`\cellx1`, mib))
		bomb(t, "cell definitions stop at MaxNodes", 4*mib, ErrLimit, in, Limits{MaxNodes: 1 << 16})
	})
	t.Run("style flood", func(t *testing.T) {
		var b strings.Builder
		b.WriteString(head + `{\stylesheet`)
		for i := 0; b.Len() < 8*mib; i++ {
			b.WriteString(`{\s` + strconv.Itoa(i) + ` n;}`)
		}
		b.WriteString(`}}`)
		bomb(t, "style definitions stop at MaxNodes", 4*mib, ErrLimit, []byte(b.String()), Limits{MaxNodes: 1 << 12})
	})
	t.Run("list flood", func(t *testing.T) {
		in := []byte(head + `{\*\listtable` + strings.Repeat(`{\list\listid1}`, mib) + `}}`)
		bomb(t, "list definitions stop at MaxNodes", 4*mib, ErrLimit, in, Limits{MaxNodes: 1 << 12})
	})
	t.Run("text flood", func(t *testing.T) {
		in := []byte(head + strings.Repeat("a", 8*mib) + `}`)
		bomb(t, "text stops at MaxText", 64<<10, ErrLimit, in, Limits{MaxText: 64 << 10})
	})
	t.Run("footnote flood", func(t *testing.T) {
		in := []byte(head + strings.Repeat(`{\footnote a}`, mib) + `}`)
		bomb(t, "footnotes stop at MaxNodes", 4*mib, ErrLimit, in, Limits{MaxNodes: 1 << 12})
	})
	t.Run("input size", func(t *testing.T) {
		in := []byte(head + strings.Repeat("a", 4*mib) + `}`)
		bomb(t, "input past MaxInput is refused before it is read", 4<<10, ErrLimit, in, Limits{MaxInput: mib})
	})
}

// The specification allows a parameter ten digits long; a hostile file has 8
// million. It is not a refusal (the document is valid RTF), so bombtest, which
// asserts one, does not apply: the property is that it is read in one pass,
// without growing, and the word still means what its clamped value says.
func TestParameterBomb(t *testing.T) {
	in := []byte(`{\rtf1 \b` + strings.Repeat("9", 8*mib) + ` x}`)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	d, err := Parse(in, Limits{})
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if got := dump(d); got != `P[<b>x</b>]` {
		t.Errorf("got %s", got)
	}
	if asked := after.TotalAlloc - before.TotalAlloc; asked > mib {
		t.Errorf("an 8 MB parameter asked the allocator for %d bytes", asked)
	}
}

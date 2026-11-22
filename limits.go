// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package rtf

import (
	"errors"
	"fmt"
)

// ErrLimit is what [Parse] returns, wrapped, when a file asks for more than
// [Limits] allows. Test for it with [errors.Is].
var ErrLimit = errors.New("rtf: limit exceeded")

// ErrNotRTF is returned for input that does not open with "{\rtf".
var ErrNotRTF = errors.New("rtf: not an RTF document")

// ErrSyntax is returned for a \bin whose length cannot be honoured: negative,
// or longer than the input that remains.
var ErrSyntax = errors.New("rtf: malformed binary data")

// Limits bound what a file may ask for. A zero field means the default, so
// Limits{} is the default everywhere.
//
// Every limit is a REFUSAL: Parse returns an error wrapping [ErrLimit] and no
// document. A reader that quietly truncated would hand back a document that
// looks complete and is not.
type Limits struct {
	// MaxInput is the largest source, in bytes.
	MaxInput int

	// MaxDepth is how deeply groups may nest. The reader keeps its own stack
	// (it does not recurse), so this bounds memory, not the Go stack: one saved
	// state per level.
	MaxDepth int

	// MaxBin is the largest a \binN payload may be, in bytes. N is a 32-bit
	// parameter, so a six-byte control word can name two gigabytes; it is also
	// refused when it names more than the input that remains, whatever this is.
	MaxBin int

	// MaxPict is the largest a single picture may be once decoded, in bytes,
	// whether it arrives as hex digits or as \bin data.
	MaxPict int

	// MaxNodes is how many blocks and inlines the document may hold. This is
	// what bounds "\par\par\par..." and "\cell\cell...", which cost four bytes of
	// input and a node of output each.
	MaxNodes int

	// MaxText is the total bytes of text the document may carry.
	MaxText int
}

const (
	defaultMaxInput = 256 << 20
	defaultMaxDepth = 256
	defaultMaxBin   = 64 << 20
	defaultMaxPict  = 64 << 20
	defaultMaxNodes = 4 << 20
	defaultMaxText  = 256 << 20

	// maxParam is the range of a control word's numeric parameter. The
	// specification allows up to ten digits (RTF 1.9.1, "Control Word"), which
	// does not fit a 32-bit int on every platform and is not meant to: it is
	// clamped to the signed 32-bit range while it is read.
	maxParam = 1<<31 - 1
	minParam = -(1 << 31)

	// maxMeta bounds each metadata string; a title is a line, not a chapter.
	maxMeta = 4096
	// maxInst bounds a field instruction; a URL is not longer than this.
	maxInst = 8192
	// maxStyleName bounds one style name.
	maxStyleName = 256
)

func (l Limits) orDefaults() Limits {
	if l.MaxInput <= 0 {
		l.MaxInput = defaultMaxInput
	}
	if l.MaxDepth <= 0 {
		l.MaxDepth = defaultMaxDepth
	}
	if l.MaxBin <= 0 {
		l.MaxBin = defaultMaxBin
	}
	if l.MaxPict <= 0 {
		l.MaxPict = defaultMaxPict
	}
	if l.MaxNodes <= 0 {
		l.MaxNodes = defaultMaxNodes
	}
	if l.MaxText <= 0 {
		l.MaxText = defaultMaxText
	}
	return l
}

// limitErr builds the refusal.
func limitErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrLimit, fmt.Sprintf(format, args...))
}

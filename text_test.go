// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package rtf

import (
	"errors"
	"testing"
)

// Text reaches the body three ways (a run of plain bytes, a \'hh escape, a \uN
// word); the text cap must hold for all of them, not only for the first.
func TestTextCapOnEscapes(t *testing.T) {
	for name, in := range map[string]string{
		"hex escapes":   `{\rtf1 \'41\'41\'41\'41}`,
		"unicode words": `{\rtf1 \u66?\u66?\u66?\u66?}`,
	} {
		if _, err := Parse([]byte(in), Limits{MaxText: 3}); !errors.Is(err, ErrLimit) {
			t.Errorf("%s: got %v, want ErrLimit", name, err)
		}
	}
}

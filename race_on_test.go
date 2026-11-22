// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

//go:build race

package rtf

// raceEnabled: the race detector makes the amplification table minutes long at
// its largest sizes, and it does not change what is allocated.
const raceEnabled = true

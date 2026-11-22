# rtf

A pure-Go RTF (Rich Text Format) reader that produces a
[richdoc](https://github.com/go-richdoc/richdoc) document, so that RTF can be
rendered, converted and diffed by everything else in the go-richdoc family
(for example `go-pdfkit/doc2pdf`).

```go
doc, err := rtf.Parse(src, rtf.Limits{}) // zero Limits = the defaults below
if errors.Is(err, rtf.ErrLimit) {
	// the file asked for more than it is allowed; nothing was built
}
```

It reads. It never executes, loads, follows or embeds anything: OLE `\object`
groups, shapes, fields other than hyperlinks, headers, footers and comments are
skipped.

## What is mapped

Paragraphs, bold, italic, strike (`\strike`), hidden text (dropped), headings
(`\outlinelvlN`, or a style named `heading N`), bulleted and numbered lists
(`\listtable` / `\ls` / `\ilvl`, and the old `\pn` form, with start numbers and
nesting), tables (`\trowd`, `\cellx`, `\intbl`, `\cell`, `\row`, header rows,
horizontal and vertical merges), hyperlinks (`HYPERLINK` fields, http, https and
mailto only), footnotes, bookmarks, PNG and JPEG pictures (hex and `\bin`),
`\title` and `\author`, all of `\'hh`, `\uN` (with `\ucN` fallback skipping and
surrogate pairs), `\ansicpg` and per-font `\fcharset` code pages (via
`golang.org/x/text`), and the special characters (`\~`, `\_`, `\emdash`, ...).

## What it cannot carry

Paragraph alignment (`\qc`, `\qr`, `\qj`; `\ql` and `\pard` reset it) is kept as
a class on the paragraph: `align-center`, `align-right` or `align-justify`. richdoc
v0.10.0 has a `Classes` field on a paragraph and nothing richer, so a renderer
decides what the class looks like. Picture sizes were already carried.

richdoc has no place for the rest, so they are dropped, not approximated:
underline, font size, colour, fonts, indents, per-column table alignment,
footnote numbers, headers and footers, and the WMF and EMF pictures. `\page` is
mapped to a thematic break outside a table, the nearest thing the model has, and
to nothing inside a cell. Nested tables are flattened into the cell that holds
them. Text after a `\row` is after the table; text in a row that never ends
becomes a paragraph, not a table.

A list that starts at 0 (`\levelstartat0`) is counted from 0 here, but richdoc
reads a start of 0 as "not set", so a consumer shows it from 1. A list resumed
after k items starts at k.

A pixel check is not made: a PNG or JPEG is kept when its signature and its
header decode (`image/png`, `image/jpeg`, `DecodeConfig`), and decoding the
pixels, or bounding the picture's size, is the caller's.

## Bounds

Every limit refuses: `Parse` returns an error wrapping `ErrLimit` (or
`ErrSyntax` for a `\bin` that cannot be honoured) and no document.

| Limit | Default | Guards |
|---|---|---|
| `MaxInput` | 16 MiB | source size, checked before reading (256 MiB before v0.1.2); raise it for a larger source |
| `MaxDepth` | 256 | group nesting; the reader uses its own stack, never recursion |
| `MaxBin` | 64 MiB | one `\binN` payload; N is clamped to 32 bits and to the bytes left in the input |
| `MaxPict` | 64 MiB | one picture, hex or binary, summed across chunks |
| `MaxNodes` | 1 Mi | blocks, inlines, cells, styles and lists, so definition floods count (4 Mi before v0.1.2) |
| `MaxText` | 256 MiB | total text, checked before a run is copied |

**Amplification.** Under the default limits, `Parse` allocates at most **80
bytes for each byte of input, plus 256 KiB** whatever the input. The worst input
known, differently formatted one-letter runs (`\b\strike`, then `\i a\i0 a`
repeated, 7 nodes in 9 bytes), costs 65 times its size at worst; one footnote or
empty cell after another, 50 to 60; a document of ASCII text costs 3, one of
accents 7 to 8. The constant is that worst with a margin of about a fifth.

The method is a sweep across growth boundaries. A slice that doubles leaves its
worst ratio just past the doubling, and what doubles is a count of runs, cells
or footnotes, not a number of bytes, so a table of round sizes can miss the
point by luck (one input did, at 140,000 repetitions). `TestAmplification`
tries every probe at 105 sizes, eight to each doubling of the input from 1 KiB
to 8 MiB (one, to 2 MiB, under `-race`, which is minutes slower and does not
change what is allocated), and the one input that broke the gate, by name. The probes are those
of the v0.1.1 reviews (long field instructions, differently formatted runs, long
metadata, high bytes, nested fields at depths 1, 16, 100 and 1000), repeated
short groups, paragraphs, rows and list items, footnotes, cells with no row, the
densest runs, a long run inside every capped string, and every destination. The
wrappers a run will get (bold, italic, strike) count as nodes when the run is
made.

The absolute worst case is the constant times `MaxInput`: with the defaults,
80 x 16 MiB, **about 1.25 GiB** (256 MiB before v0.1.2, which made it 20 GiB). A
reader that raises `MaxInput` or `MaxNodes` raises the figure with it, at about
160 bytes a node.

Numeric parameters saturate at 32 bits however many digits they have (an 8 MB
parameter costs one pass and under 1 MiB). Link targets pass `safeURL`.
`bomb_test.go` asserts a refusal for each limit under a held-memory cap with
`bombtest`; `FuzzParse` has a committed corpus under `testdata/fuzz`, and CI
runs it for 30 seconds on every change as well as the whole suite on linux/386.

## Against BentoPDF

BentoPDF lists "RTF to PDF: Convert RTF (Rich Text Format) documents to PDF."
(`src/js/logic/tools.ts`, BentoPDF commit `f1d685bc`, 2026-11-19). It has no
RTF parser: `src/js/logic/rtf-to-pdf-page.ts` (242 lines) hands the file to
LibreOffice compiled to WebAssembly and downloads the result.

This package is the reading half of the same job. It matches the input
(RTF in) and not LibreOffice's layout: richdoc carries structure, not page
geometry, so a PDF made from it is a clean document with headings, lists,
tables and links, not a pixel copy of Word's pagination. In exchange it is a
small pure-Go dependency (no WASM engine, no CGO), every size is bounded, and
the parser is fuzzed.

What BentoPDF has and this does not: Writer's whole layout, that is font sizes,
colours, underline, indents, headers and footers, WMF and EMF pictures, text in
frames laid out where it was put, and page geometry. Here only structure,
alignment and picture size survive.

## Measured

`go test -bench . -cpu 1` on an Apple M4 Max, a 1 MB synthetic document (styled
runs, `\'hh` and `\uN`, a table row per paragraph):

| | allocs/op | B/op |
|---|---|---|
| 1 MB synthetic, v0.1.0 | 568,007 | 16.2 MB |
| 1 MB synthetic, v0.1.1 | 305,874 | 13.1 MB |
| 1 MB synthetic, v0.1.2 | 236,352 | 10.8 MB |

`TestAllocationsOfAMegabyte` holds a ceiling of 250,000 allocations on that
document. Allocation counts are exact. Times were taken at a 1-minute load far
past the point where times mean anything, so none is published. The reference
for correctness is LibreOffice (below); a speed comparison with it would be a
comparison with a whole office suite, not a parser, so none is claimed and this
is an open item, not a result.

## Against LibreOffice

`go test -tags looracle -run TestLibreOfficeOracle -v` converts every fixture
with `soffice --headless --convert-to txt` and compares words with
`richdoc.PlainText` of our result. It is a developer's check: CI does not run
it, and it skips when LibreOffice is not installed (`$SOFFICE` or the macOS
application path). On the 13 fixtures, run with LibreOffice from
`/Applications` on 2026-11-22: ten give the same words, LibreOffice adding only
the labels of list items (a bullet, `1.`), and three (`fdo47036`, `fdo49892`,
`fdo57708`) differ because their text is in a positioned frame (`\posx`), which
LibreOffice's text export leaves out and this reader keeps. Outside frames, every
word of ours is in LibreOffice's output.

## Testing

100% statement coverage is enforced in CI, along with gofmt, vet, `-race`,
govulncheck and a cross-build for 12 targets. Fixtures, their sources and
licences, and the honest gaps (no Word fixture) are in
[`testdata/README.md`](testdata/README.md).

## Licence

BSD-3-Clause.

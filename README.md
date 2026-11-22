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

richdoc has no place for these, so they are dropped, not approximated:
underline, font size, colour, alignment, indents, footnote numbers. `\page` is
mapped to a thematic break, the nearest thing the model has. Nested tables are
flattened into the cell that holds them. Text in a table row that never ends
becomes a paragraph, not a table.

## Bounds

Every limit refuses: `Parse` returns an error wrapping `ErrLimit` (or
`ErrSyntax` for a `\bin` that cannot be honoured) and no document.

| Limit | Default | Guards |
|---|---|---|
| `MaxInput` | 256 MiB | source size, checked before reading |
| `MaxDepth` | 256 | group nesting; the reader uses its own stack, never recursion |
| `MaxBin` | 64 MiB | one `\binN` payload; N is clamped to 32 bits and to the bytes left in the input |
| `MaxPict` | 64 MiB | one picture, hex or binary, summed across chunks |
| `MaxNodes` | 4 Mi | blocks, inlines, cells, styles and lists, so definition floods count |
| `MaxText` | 256 MiB | total text, checked before a run is copied |

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

## Measured

`go test -bench . -cpu 1` on an Apple M4 Max, a 1 MB synthetic document (styled
runs, `\'hh` and `\uN`, a table row per paragraph):

| | allocs/op | B/op |
|---|---|---|
| 1 MB synthetic | 576,743 | 16.4 MB |
| 4.9 KB textutil sample | 240 | 8.8 KB |

Allocation counts are exact. Times were taken at a 1-minute load of about 30,
which is past the point where times mean anything, so none is published: the
first reading (47 MB/s) is an upper bound on the cost, not a measurement. There
is no reference RTF parser installed on the machine (`pandoc`, `unrtf` and
`soffice` are absent; `textutil` only converts, to its own model), so no
comparison is claimed and this is an open item, not a result. The allocation
rate (about 0.55 allocations per input byte of a dense file) is the obvious
target.

## Testing

100% statement coverage is enforced in CI, along with gofmt, vet, `-race`,
govulncheck and a cross-build for 12 targets. Fixtures, their sources and
licences, and the honest gaps (no Word fixture) are in
[`testdata/README.md`](testdata/README.md).

## Licence

BSD-3-Clause.

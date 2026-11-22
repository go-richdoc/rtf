# Fixtures

Every `.rtf` here was written by another program. Each has a `.golden` next to
it: what this reader makes of it, pinned by `TestFixtures`. A golden is reviewed
by reading it (`go test -run TestFixtures -update`, then read the diff), never
accepted because it was generated.

## libreoffice/

Twelve files fetched from the LibreOffice core repository,
`sw/qa/extras/rtfimport/data/`
(https://github.com/LibreOffice/core/tree/master/sw/qa/extras/rtfimport/data):
`fdo47036`, `fdo49892`, `fdo57708`, `fdo66565`, `fdo67365`, `tdf84684`,
`tdf91684`, `tdf112211-2`, `tdf115242`, `paste-first-para-direct-format`,
`classification-yes`, `ink`.

Licence: the files carry no header of their own. They live in a repository that
GitHub reports as GPL-3.0 (its `COPYING`), whose README describes the project as
"based on copyleft licenses", and whose contribution policy is MPL-2.0. We have
not found a statement for these particular files, so none is claimed here beyond
that: they are test inputs, read by `go test` and never compiled into or
distributed with the library. Remove the directory if that is not acceptable.

## textutil/

`source.html` is written for this repository (BSD-3-Clause, like the rest).
`sample.rtf` is what macOS `textutil -convert rtf source.html -output
sample.rtf` made of it, so it is Apple-produced RTF of known content: that is
the one fixture whose expected result does not come from this reader
(`TestTextutilSample` compares against the HTML source).

There is NO Microsoft Word fixture. None was obtainable with a licence we could
state, and a hand-written "Word-like" file would prove nothing.

## fuzz/FuzzParse/

The committed fuzz corpus: three shapes that matter (deep nesting, a `\bin`
longer than the input, `\uN` skip counts around surrogates). `FuzzParse` also
seeds itself from every fixture above.

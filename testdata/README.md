# Fixtures

Every `.rtf` here was written by another program. Each has a `.golden` next to
it: what this reader makes of it, pinned by `TestFixtures`. A golden is reviewed
by reading it (`go test -run TestFixtures -update`, then read the diff), never
accepted because it was generated.

## libreoffice/

Twelve files from the LibreOffice core repository,
`sw/qa/extras/rtfimport/data/`
(https://github.com/LibreOffice/core/tree/master/sw/qa/extras/rtfimport/data).

**Licence: MPL-2.0, applied file by file, with its text in
[`libreoffice/LICENSE`](libreoffice/LICENSE).** The basis is the project's own
statement at https://www.libreoffice.org/about-us/licenses/: "LibreOffice is
made available subject to the terms of the Mozilla Public License v2.0". Read
2026-11-22. That page does not list exceptions in the source tree, and the
files carry no header of their own, so MPL-2.0 is the licence we rely on and
state; if LibreOffice says otherwise for one of them, remove it.

The repository's root `COPYING` is the GPL-3 text, which is why GitHub labels
the repository GPL-3.0; it is not the licence of the project as the page above
states it. An earlier version of this file said the fixtures are "never
distributed". That was wrong: a Go module zip includes `testdata`, so they ship
with this module, and the MPL-2.0 file in that directory travels with them.
Under MPL-2.0 these files stay under MPL-2.0 (section 3); the Go code of this
module, which only reads them, stays BSD-3-Clause.

Each file, with the LibreOffice commit that last changed it on `master` (read
from the GitHub API on 2026-11-22) and whether this copy is the same blob as
`master`'s (`git hash-object --no-filters` against the tree listing):

| file | last commit | date | blob equals master |
|---|---|---|---|
| `classification-yes.rtf` | `b1f9369b8181` | 2016-03-10 | yes |
| `fdo47036.rtf` | `06b6a1da4a19` | 2012-07-13 | yes |
| `fdo49892.rtf` | `881a08079cee` | 2012-08-02 | yes |
| `fdo57708.rtf` | `7baca04727fb` | 2012-11-30 | yes |
| `fdo66565.rtf` | `f6a00c88482e` | 2013-07-09 | yes |
| `fdo67365.rtf` | `4663b9e494ab` | 2013-12-23 | yes |
| `ink.rtf` | `045227f10568` | 2012-09-19 | yes |
| `paste-first-para-direct-format.rtf` | `aff70ac6ce33` | 2020-10-07 | yes |
| `tdf112211-2.rtf` | `295cf49b81b8` | 2017-10-10 | yes |
| `tdf115242.rtf` | `03cee02464f2` | 2018-02-06 | yes |
| `tdf84684.rtf` | `5edebdf67720` | 2016-09-20 | yes |
| `tdf91684.rtf` | `c3acc0623016` | 2016-06-14 | yes |

"Yes" is for the bytes as downloaded. Three of them (`fdo49892`, `fdo57708`,
`ink`) use CRLF line ends, which `.gitattributes` (`testdata/** text eol=lf`)
turns into LF in this repository; RTF treats a line end as nothing, and the
goldens are the same either way. The `.golden` files are ours.

`TestLibreOfficeOracle` (`oracle_test.go`, tag `looracle`, not run by CI)
compares each fixture's words with what an installed LibreOffice exports; see
the README at the root.

## textutil/

`source.html` is written for this repository (BSD-3-Clause, like the rest).
`sample.rtf` is what macOS `textutil -convert rtf source.html -output
sample.rtf` made of it, so it is Apple-produced RTF of known content: that is
the one fixture whose expected result does not come from this reader
(`TestTextutilSample` compares against the HTML source).

There is NO Microsoft Word fixture. None was obtainable with a licence we could
state, and a hand-written "Word-like" file would prove nothing.

## fuzz/FuzzParse/

The committed fuzz corpus: the shapes that matter (deep nesting, a `\bin`
longer than the input, `\uN` skip counts around surrogates, and one file per
defect found in the review of v0.1.0). `FuzzParse` also seeds itself from every
fixture above.

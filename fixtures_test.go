// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package rtf

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files from what the reader produces")

// Every file under testdata is real RTF written by another program (see
// testdata/README.md for each one's source and licence). The reader must accept
// all of them, and what it makes of each is pinned in a .golden file that is
// reviewed by reading it, not generated and trusted: run with -update, then read
// the diff.
func TestFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/*/*.rtf")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, f := range files {
		t.Run(strings.TrimSuffix(filepath.Base(f), ".rtf"), func(t *testing.T) {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			d, err := Parse(src, Limits{})
			if err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			got := dump(d)
			for k, v := range d.Meta {
				got += "\n" + k + "=" + v
			}
			got += "\n"
			golden := strings.TrimSuffix(f, ".rtf") + ".golden"
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run with -update and read the result)", err)
			}
			if string(want) != got {
				t.Errorf("%s changed:\n got  %s\n want %s", f, got, want)
			}
		})
	}
}

// The textutil sample was made from source.html, so what is in the source must
// be in the result: that is the oracle that does not come from this reader.
func TestTextutilSample(t *testing.T) {
	src, err := os.ReadFile("testdata/textutil/sample.rtf")
	if err != nil {
		t.Fatal(err)
	}
	d, err := Parse(src, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	got := dump(d)
	for _, want := range []string{
		"Quarterly report", "<b>bold</b>", "<i>italic</i>", "<s>strike</s>",
		"café", "€", `<a https://example.org/page "">link to a page</a>`,
		`<a mailto:a@example.org "">mail link</a>`,
		"}T{<b>Name</b>|<b>Value</b>;alpha|1;beta|2;}", "first bullet", "second step", "alpha", "beta", "☃", "日本",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("%q is missing from %s", want, got)
		}
	}
}

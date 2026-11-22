// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

//go:build looracle

package rtf

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-richdoc/richdoc"
)

// TestLibreOfficeOracle is a developer's check, outside CI: it asks a real
// LibreOffice what the text of each fixture is and compares it, word by word,
// with what this reader makes of it.
//
//	go test -tags looracle -run TestLibreOfficeOracle -v
//
// LibreOffice is found at $SOFFICE, or at the macOS application path. Words are
// compared, not layout: LibreOffice writes a table as tab-separated lines and a
// list with its labels, this package keeps structure and none of that. The
// fixture is read as a whole, so an unsupported feature shows as a difference.
// The test fails only on a word LibreOffice does not have, which would be text
// this reader made up, unless the fixture has text in a frame; words only
// LibreOffice has (list labels such as a bullet or "1.") are logged.
func TestLibreOfficeOracle(t *testing.T) {
	soffice := os.Getenv("SOFFICE")
	if soffice == "" {
		soffice = "/Applications/LibreOffice.app/Contents/MacOS/soffice"
	}
	if _, err := os.Stat(soffice); err != nil {
		t.Skipf("no LibreOffice: %v", err)
	}
	files, err := filepath.Glob("testdata/*/*.rtf")
	if err != nil || len(files) == 0 {
		t.Fatalf("fixtures: %v %v", files, err)
	}
	out := t.TempDir()
	profile := "-env:UserInstallation=file://" + t.TempDir()
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".rtf")
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(soffice, profile, "--headless", "--convert-to", "txt:Text (encoded):UTF8", "--outdir", out, f)
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("soffice: %v\n%s", err, b)
			}
			want, err := os.ReadFile(filepath.Join(out, name+".txt"))
			if err != nil {
				t.Fatal(err)
			}
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := Parse(src, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			// The text filter writes a byte-order mark first.
			bom := string(rune(0xFEFF))
			lo, ours := strings.Fields(strings.TrimPrefix(string(want), bom)), strings.Fields(richdoc.PlainText(doc))
			have := map[string]int{}
			for _, w := range lo {
				have[w]++
			}
			var invented []string
			for _, w := range ours {
				if have[w] == 0 {
					invented = append(invented, w)
					continue
				}
				have[w]--
			}
			missing := 0
			for _, n := range have {
				missing += n
			}
			t.Logf("LibreOffice %d words, ours %d, only ours %d %q, only LibreOffice %d", len(lo), len(ours), len(invented), invented, missing)
			if len(invented) > 0 || missing > 0 {
				t.Logf("LibreOffice %.300q\n    ours        %.300q", want, richdoc.PlainText(doc))
			}
			// A positioned paragraph (\posx) is text in a frame, which the text
			// filter leaves out and this reader keeps: three fixtures hold one.
			if len(invented) > 0 && bytes.Contains(src, []byte(`\posx`)) {
				t.Logf("text in a frame, which LibreOffice's text export omits")
				return
			}
			if len(invented) > 0 {
				t.Errorf("words LibreOffice does not have: %q", invented)
			}
		})
	}
}

package comb

import "testing"

func TestParseNumstat(t *testing.T) {
	out := "2\t1\ttext.txt\n-\t-\timage.png\n10\t0\tname-with-tab\tinside.txt\n"
	want := ShortStat{FilesChanged: 3, Insertions: 12, Deletions: 1}
	got, err := parseNumstat(out)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("parseNumstat = %+v, want %+v", got, want)
	}
}

func TestParseNumstatRejectsMalformedOutput(t *testing.T) {
	for _, out := range []string{"not numstat\n", "x\t1\tfile.txt\n"} {
		if _, err := parseNumstat(out); err == nil {
			t.Errorf("parseNumstat(%q) accepted malformed output", out)
		}
	}
}

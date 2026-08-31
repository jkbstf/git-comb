package comb

import "testing"

func TestParseSignSet(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "DUS", want: "DUS"},
		{in: "dus", want: "DUS"},
		{in: "sud", want: "DUS"}, // canonical order restored
		{in: "D,U S", want: "DUS"},
		{in: "DDU", want: "DU"}, // duplicates collapse
		{in: "DUABSELO", want: "DUABSELO"},
		{in: "", wantErr: true},
		{in: ",", wantErr: true},
		{in: "DX", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseSignSet(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseSignSet(%q) accepted, want error", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSignSet(%q): %v", tt.in, err)
			}
			if got.String() != tt.want {
				t.Errorf("ParseSignSet(%q) = %q, want %q", tt.in, got.String(), tt.want)
			}
		})
	}
}

func TestSignSetHasAndFilter(t *testing.T) {
	var all SignSet
	if !all.All() || !all.Has('B') {
		t.Error("zero SignSet must select everything")
	}
	if got := all.Filter("DUAB"); got != "DUAB" {
		t.Errorf("all.Filter = %q, want DUAB", got)
	}

	only, err := ParseSignSet("DS")
	if err != nil {
		t.Fatal(err)
	}
	if only.All() {
		t.Error("DS reported as all")
	}
	if !only.Has('D') || !only.Has('S') || only.Has('U') {
		t.Errorf("Has() wrong for %q", only.String())
	}
	if got := only.Filter("DUABS"); got != "DS" {
		t.Errorf("Filter(DUABS) = %q, want DS", got)
	}
	if got := only.Filter("UAB"); got != "" {
		t.Errorf("Filter(UAB) = %q, want empty", got)
	}

	hidden, err := ParseSignSet("AB")
	if err != nil {
		t.Fatal(err)
	}
	rest := hidden.Complement()
	if got, want := rest.String(), "DUSELO"; got != want {
		t.Errorf("Complement = %q, want %q", got, want)
	}
	if rest.Has('A') || !rest.Has('D') {
		t.Error("Complement membership wrong")
	}

	// Excluding everything is a valid selection that finds nothing.
	empty := (SignSet{}).Complement()
	if empty.All() {
		t.Error("complement of the full set claims to select all")
	}
	for i := 0; i < len(signOrder); i++ {
		if empty.Has(signOrder[i]) {
			t.Errorf("empty selection has %q", signOrder[i])
		}
	}
	if got := empty.Filter("DUS"); got != "" {
		t.Errorf("empty selection filtered %q, want empty", got)
	}

	full, err := ParseSignSet("DUABSELO")
	if err != nil {
		t.Fatal(err)
	}
	if !full.All() {
		t.Error("the explicit full set must equal the zero value")
	}
}

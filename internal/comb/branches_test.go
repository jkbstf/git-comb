package comb

import (
	"strconv"
	"strings"
	"testing"
)

func TestParseTracking(t *testing.T) {
	tests := []struct {
		description   string
		ahead, behind int
		gone          bool
	}{
		{"", 0, 0, false},
		{"ahead 12", 12, 0, false},
		{"behind 7", 0, 7, false},
		{"ahead 5, behind 3", 5, 3, false},
		{"gone", 0, 0, true},
	}
	for _, tt := range tests {
		ahead, behind, gone, err := parseTracking(tt.description)
		if err != nil {
			t.Errorf("parseTracking(%q): %v", tt.description, err)
			continue
		}
		if ahead != tt.ahead || behind != tt.behind || gone != tt.gone {
			t.Errorf("parseTracking(%q) = %d, %d, %v; want %d, %d, %v",
				tt.description, ahead, behind, gone, tt.ahead, tt.behind, tt.gone)
		}
	}
	for _, malformed := range []string{
		"ahead one, behind 2", "forward 1", "ahead 1 behind",
	} {
		if _, _, _, err := parseTracking(malformed); err == nil {
			t.Errorf("parseTracking(%q) accepted malformed input", malformed)
		}
	}
}

func TestParseLocalOnlyGraphCountsOverlappingBranches(t *testing.T) {
	refs := []branchRef{{Name: "left", OID: "left"}, {Name: "right", OID: "right"}}
	graph := "left shared\nright shared\nshared remote-boundary\n"
	total, counts, err := parseLocalOnlyGraph(graph, refs)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || counts["left"] != 2 || counts["right"] != 2 {
		t.Errorf("total, counts = %d, %v; want 3, map[left:2 right:2]", total, counts)
	}
}

func TestParseLocalOnlyGraphSupportsMoreThanOneMaskWord(t *testing.T) {
	refs := make([]branchRef, 70)
	for i := range refs {
		refs[i] = branchRef{Name: string(rune('a' + i)), OID: "shared"}
	}
	total, counts, err := parseLocalOnlyGraph("shared\n", refs)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	for _, ref := range refs {
		if counts[ref.Name] != 1 {
			t.Errorf("count[%q] = %d, want 1", ref.Name, counts[ref.Name])
		}
	}
}

func BenchmarkParseLocalOnlyGraph(b *testing.B) {
	const (
		commits  = 100_000
		branches = 70
	)
	refs := make([]branchRef, branches)
	for i := range refs {
		refs[i] = branchRef{Name: "branch-" + strconv.Itoa(i), OID: "c99999"}
	}
	var graph strings.Builder
	for i := commits - 1; i >= 0; i-- {
		graph.WriteByte('c')
		graph.WriteString(strconv.Itoa(i))
		if i > 0 {
			graph.WriteString(" c")
			graph.WriteString(strconv.Itoa(i - 1))
		}
		graph.WriteByte('\n')
	}
	out := graph.String()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		total, counts, err := parseLocalOnlyGraph(out, refs)
		if err != nil {
			b.Fatal(err)
		}
		if total != commits || counts[refs[0].Name] != commits {
			b.Fatalf("total, first count = %d, %d", total, counts[refs[0].Name])
		}
	}
}

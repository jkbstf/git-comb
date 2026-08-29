package comb

import "testing"

func TestParseStatus(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want worktreeStatus
	}{
		{
			name: "clean synced branch",
			out: "# branch.oid 4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d\n" +
				"# branch.head master\n" +
				"# branch.upstream origin/master\n" +
				"# branch.ab +0 -0\n",
			want: worktreeStatus{
				Branch:      "master",
				OID:         "4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d",
				HasUpstream: true,
			},
		},
		{
			name: "ahead and behind",
			out: "# branch.oid 4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d\n" +
				"# branch.head master\n" +
				"# branch.upstream origin/master\n" +
				"# branch.ab +3 -2\n",
			want: worktreeStatus{
				Branch:      "master",
				OID:         "4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d",
				HasUpstream: true,
				Ahead:       3,
				Behind:      2,
			},
		},
		{
			name: "no upstream omits the ab line",
			out: "# branch.oid 4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d\n" +
				"# branch.head feature\n",
			want: worktreeStatus{
				Branch: "feature",
				OID:    "4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d",
			},
		},
		{
			name: "untracked file is dirty",
			out: "# branch.oid 4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d\n" +
				"# branch.head master\n" +
				"? new.txt\n",
			want: worktreeStatus{
				Branch: "master",
				OID:    "4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d",
				Dirty:  true,
			},
		},
		{
			name: "staged change is dirty",
			out: "# branch.oid 4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d\n" +
				"# branch.head master\n" +
				"1 M. N... 100644 100644 100644 aaaa bbbb file.txt\n",
			want: worktreeStatus{
				Branch: "master",
				OID:    "4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d",
				Dirty:  true,
			},
		},
		{
			name: "renamed entry is dirty",
			out: "# branch.oid 4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d\n" +
				"# branch.head master\n" +
				"2 R. N... 100644 100644 100644 aaaa bbbb R100 new.txt\told.txt\n",
			want: worktreeStatus{
				Branch: "master",
				OID:    "4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d",
				Dirty:  true,
			},
		},
		{
			name: "unmerged entry is dirty",
			out: "# branch.oid 4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d\n" +
				"# branch.head master\n" +
				"u UU N... 100644 100644 100644 100644 aaaa bbbb cccc conflicted.txt\n",
			want: worktreeStatus{
				Branch: "master",
				OID:    "4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d",
				Dirty:  true,
			},
		},
		{
			name: "ignored entry is not dirty",
			out: "# branch.oid 4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d\n" +
				"# branch.head master\n" +
				"! ignored.log\n",
			want: worktreeStatus{
				Branch: "master",
				OID:    "4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d",
			},
		},
		{
			name: "empty repository",
			out: "# branch.oid (initial)\n" +
				"# branch.head master\n",
			want: worktreeStatus{
				Branch: "master",
				Empty:  true,
			},
		},
		{
			name: "detached head",
			out: "# branch.oid 4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d\n" +
				"# branch.head (detached)\n",
			want: worktreeStatus{
				OID:      "4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d4a3b2c1d",
				Detached: true,
			},
		},
		{
			name: "empty output",
			out:  "",
			want: worktreeStatus{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseStatus(tt.out); got != tt.want {
				t.Errorf("parseStatus:\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestParseAheadBehind(t *testing.T) {
	tests := []struct {
		in            string
		ahead, behind int
	}{
		{"+0 -0", 0, 0},
		{"+3 -2", 3, 2},
		{"+12 -0", 12, 0},
		{"garbage", 0, 0},
		{"", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			ahead, behind := parseAheadBehind(tt.in)
			if ahead != tt.ahead || behind != tt.behind {
				t.Errorf("parseAheadBehind(%q) = %d, %d; want %d, %d",
					tt.in, ahead, behind, tt.ahead, tt.behind)
			}
		})
	}
}

func TestDescribeBranch(t *testing.T) {
	tests := []struct {
		name string
		st   worktreeStatus
		want string
	}{
		{"branch", worktreeStatus{Branch: "master"}, "master"},
		{"detached with oid", worktreeStatus{Detached: true, OID: "4a3b2c1d4a3b"}, "detached@4a3b2c1"},
		{"detached without oid", worktreeStatus{Detached: true}, "detached"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := describeBranch(tt.st); got != tt.want {
				t.Errorf("describeBranch = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSignsOrderAndClean(t *testing.T) {
	all := Report{
		Dirty:       true,
		Unpushed:    1,
		Ahead:       true,
		Behind:      true,
		Stashes:     1,
		Empty:       true,
		NoRemote:    true,
		FetchFailed: true,
	}
	if got, want := all.Signs(), "DUABSENR"; got != want {
		t.Errorf("Signs() = %q, want %q", got, want)
	}
	if all.Clean() {
		t.Error("Clean() = true for a report full of findings")
	}
	if !(Report{}).Clean() {
		t.Error("Clean() = false for an empty report")
	}
}

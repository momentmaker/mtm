package words

import "testing"

func TestCount(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{{0, "0 stashes"}, {1, "1 stash"}, {3, "3 stashes"}}
	for _, tt := range tests {
		if got := Count(tt.n, "stash", "stashes"); got != tt.want {
			t.Errorf("Count(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestBytes(t *testing.T) {
	tests := []struct {
		n    uint64
		want string
	}{{0, "0 B"}, {999, "999 B"}, {1000, "1.0 kB"}, {224_300_000, "224.3 MB"}, {13_136_968_000, "13.1 GB"}, {10_000_000_000, "10.0 GB"}}
	for _, tt := range tests {
		if got := Bytes(tt.n); got != tt.want {
			t.Errorf("Bytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

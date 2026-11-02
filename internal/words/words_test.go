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

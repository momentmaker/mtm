package pathset

import "testing"

func TestMatch(t *testing.T) {
	set := MustNew(
		"Library/Caches/",
		"Library/Containers/*/Data/Library/Caches/",
		"Library/Saved Application State/",
		".DS_Store",
		".zcompdump*",
		"~/Library/Application Support/Foo/*.log",
	)
	tests := []struct {
		rel  string
		want bool
	}{
		{"Library/Caches", true},
		{"Library/Caches/com.apple.Safari/Cache.db", true},
		{"Library/CachesBackup/x", false},
		{"Library", false},
		{"Library/Containers/com.docker.docker/Data/Library/Caches/x", true},
		{"Library/Containers/com.docker.docker/Data/vms", false},
		{"Library/Saved Application State/com.app.savedState/data.data", true},
		{"GitHub/app/.DS_Store", true},
		{".DS_Store", true},
		{".zcompdump-host-5.9", true},
		{"Library/Application Support/Foo/run.log", true},
		{"Library/Application Support/Foo/sub/run.log", false},
		{"GitHub/app/main.go", false},
	}
	for _, tt := range tests {
		if got := set.Match(tt.rel); got != tt.want {
			t.Errorf("Match(%q) = %v, want %v", tt.rel, got, tt.want)
		}
	}
}

func TestNewRejectsBadPattern(t *testing.T) {
	if _, err := New("["); err == nil {
		t.Error("New(\"[\") succeeded, want an error")
	}
}

func TestEmptySetMatchesNothing(t *testing.T) {
	if MustNew().Match("anything") {
		t.Error("empty set matched")
	}
}

package git

import (
	"maps"
	"slices"
	"testing"
)

func TestParseRefs(t *testing.T) {
	out := []byte("refs/heads/main aaa\nrefs/heads/feature bbb\nrefs/remotes/origin/main ccc\nrefs/stash ddd\nrefs/tags/v1 eee\n")
	want := map[string]string{"refs/heads/main": "aaa", "refs/heads/feature": "bbb", "refs/tags/v1": "eee"}
	if got := parseRefs(out); !maps.Equal(got, want) {
		t.Errorf("parseRefs = %v, want %v", got, want)
	}
	if got := parseRefs(nil); len(got) != 0 {
		t.Errorf("parseRefs(nil) = %v, want empty", got)
	}
}

func TestParseCount(t *testing.T) {
	if n, err := parseCount([]byte("3\n")); err != nil || n != 3 {
		t.Errorf("parseCount = %d, %v", n, err)
	}
	if _, err := parseCount([]byte("x")); err == nil {
		t.Error("parseCount accepted garbage")
	}
}

func TestParseStatus(t *testing.T) {
	out := []byte("# branch.oid abc\x00# branch.head main\x00" +
		"1 .M N... 100644 100644 100644 h1 h2 src/app.go\x00" +
		"2 R. N... 100644 100644 100644 h1 h2 R100 new name.txt\x00old name.txt\x00" +
		"? notes\nwith newline.txt\x00? -rf.txt\x00! .env\x00! build/\x00")
	records, untracked, ignored := parseStatus(out)
	wantRecords := []string{
		"# branch.oid abc",
		"# branch.head main",
		"1 .M N... 100644 100644 100644 h1 h2 src/app.go",
		"2 R. N... 100644 100644 100644 h1 h2 R100 new name.txt\x00old name.txt",
		"? notes\nwith newline.txt",
		"? -rf.txt",
	}
	if !slices.Equal(records, wantRecords) {
		t.Errorf("records = %q\nwant %q", records, wantRecords)
	}
	if want := []string{"notes\nwith newline.txt", "-rf.txt"}; !slices.Equal(untracked, want) {
		t.Errorf("untracked = %q, want %q", untracked, want)
	}
	if want := []string{".env", "build/"}; !slices.Equal(ignored, want) {
		t.Errorf("ignored = %q, want %q", ignored, want)
	}
}

func TestParseMissing(t *testing.T) {
	out := []byte("aaa commit 230\nbbb missing\nccc tag 140\n")
	if got := parseMissing(out); !slices.Equal(got, []string{"bbb"}) {
		t.Errorf("parseMissing = %q, want [bbb]", got)
	}
}

func TestImportantIgnored(t *testing.T) {
	in := []string{".env", "config/.env.local", "build/", "certs/dev.pem", "id.key", "out.bin", "app.local.json"}
	want := []string{".env", "config/.env.local", "certs/dev.pem", "id.key", "app.local.json"}
	if got := importantIgnored(in); !slices.Equal(got, want) {
		t.Errorf("importantIgnored = %q, want %q", got, want)
	}
}

func TestPlural(t *testing.T) {
	if got := plural(1, "stash", "stashes"); got != "1 stash" {
		t.Errorf("plural(1) = %q", got)
	}
	if got := plural(3, "stash", "stashes"); got != "3 stashes" {
		t.Errorf("plural(3) = %q", got)
	}
}

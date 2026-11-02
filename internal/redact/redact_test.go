package redact

import (
	"slices"
	"strings"
	"testing"
)

func TestArgs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"secret flag then value", []string{"--password", "hunter2", "db"}, []string{"--password", Mask, "db"}},
		{"secret flag with equals", []string{"--token=abc123"}, []string{"--token=" + Mask}},
		{"secret key=value pair", []string{"API_KEY=abc", "PATH=/bin"}, []string{"API_KEY=" + Mask, "PATH=/bin"}},
		{"ordinary flags untouched", []string{"-C", "/tmp/repo", "--format=%(refname) %(objectname)"}, []string{"-C", "/tmp/repo", "--format=%(refname) %(objectname)"}},
		{"credentials in a URL", []string{"clone", "https://me:s3cret@example.com/r.git"}, []string{"clone", "https://" + Mask + "@example.com/r.git"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Args(tt.in); !slices.Equal(got, tt.want) {
				t.Errorf("Args(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestArgsDoesNotModifyInput(t *testing.T) {
	in := []string{"--password", "hunter2"}
	Args(in)
	if in[1] != "hunter2" {
		t.Errorf("input changed to %q", in)
	}
}

func TestTextMasksKnownTokens(t *testing.T) {
	in := "auth failed for ghp_" + strings.Repeat("a", 36) + " and sk-ant-" + strings.Repeat("b", 30)
	got := Text(in)
	if strings.Contains(got, "ghp_") || strings.Contains(got, "sk-ant-") {
		t.Errorf("Text(%q) = %q, still contains a token", in, got)
	}
}

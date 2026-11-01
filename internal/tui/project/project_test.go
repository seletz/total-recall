package project

import (
	"reflect"
	"testing"
)

func TestName(t *testing.T) {
	tests := []struct {
		name    string
		gitRoot string
		cwd     string
		want    string
	}{
		{"git root", "/w/alpha", "/w/alpha", "alpha"},
		{"git root, cwd in subdirectory", "/w/alpha", "/w/alpha/internal/store", "alpha"},
		{"worktree resolved to the main repo", "/w/alpha", "/w/alpha__worktrees/feature", "alpha"},
		{"trailing slash", "/w/alpha/", "", "alpha"},
		{"no repo", "", "/home/u/vault", "~vault"},
		{"no repo, trailing slash", "", "/home/u/vault/", "~vault"},
		{"no repo, root directory", "", "/", "~/"},
		{"empty", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Name(tt.gitRoot, tt.cwd); got != tt.want {
				t.Errorf("Name(%q, %q) = %q, want %q", tt.gitRoot, tt.cwd, got, tt.want)
			}
		})
	}
}

func TestNames(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		want map[string]string
	}{
		{"nil", nil, map[string]string{}},
		{"distinct", []string{"/w/alpha", "/w/beta", "/w/gamma"},
			map[string]string{"/w/alpha": "alpha", "/w/beta": "beta", "/w/gamma": "gamma"}},
		{"collision gets the parent directory", []string{"/w/iris/iris", "/w/iris", "/w/beta"},
			map[string]string{"/w/iris/iris": "iris/iris", "/w/iris": "w/iris", "/w/beta": "beta"}},
		{"collision of three", []string{"/a/x", "/b/x", "/c/x"},
			map[string]string{"/a/x": "a/x", "/b/x": "b/x", "/c/x": "c/x"}},
		{"collision in parent too", []string{"/one/a/x", "/two/a/x", "/b/x"},
			map[string]string{"/one/a/x": "one/a/x", "/two/a/x": "two/a/x", "/b/x": "b/x"}},
		{"collision with the root directory", []string{"/x", "/a/x"},
			map[string]string{"/x": "/x", "/a/x": "a/x"}},
		{"other is kept", []string{"/w/alpha", "other"},
			map[string]string{"/w/alpha": "alpha", "other": "other"}},
		{"repo named other gets the parent directory", []string{"/w/other", "other"},
			map[string]string{"/w/other": "w/other", "other": "other"}},
		{"empty and duplicate keys", []string{"", "/w/alpha", "/w/alpha"},
			map[string]string{"": "", "/w/alpha": "alpha"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Names(tt.keys); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Names(%q) = %v, want %v", tt.keys, got, tt.want)
			}
		})
	}
}

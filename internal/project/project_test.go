package project

import (
	"reflect"
	"testing"
)

func TestNames(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		dirs []string
		home string
		want map[string]string
	}{
		{"nil", nil, nil, "", map[string]string{}},
		{"distinct", []string{"/w/alpha", "/w/beta", "/w/gamma"}, nil, "",
			map[string]string{"/w/alpha": "alpha", "/w/beta": "beta", "/w/gamma": "gamma"}},
		{"collision gets the parent directory", []string{"/w/dipa/dipa", "/w/dipa", "/w/beta"}, nil, "",
			map[string]string{"/w/dipa/dipa": "dipa/dipa", "/w/dipa": "w/dipa", "/w/beta": "beta"}},
		{"collision of three", []string{"/a/x", "/b/x", "/c/x"}, nil, "",
			map[string]string{"/a/x": "a/x", "/b/x": "b/x", "/c/x": "c/x"}},
		{"collision in parent too", []string{"/one/a/x", "/two/a/x", "/b/x"}, nil, "",
			map[string]string{"/one/a/x": "one/a/x", "/two/a/x": "two/a/x", "/b/x": "b/x"}},
		{"collision with the root directory", []string{"/x", "/a/x"}, nil, "",
			map[string]string{"/x": "/x", "/a/x": "a/x"}},
		{"other is kept", []string{"/w/alpha", "other"}, nil, "",
			map[string]string{"/w/alpha": "alpha", "other": "other"}},
		{"repo named other gets the parent directory", []string{"/w/other", "other"}, nil, "",
			map[string]string{"/w/other": "w/other", "other": "other"}},
		{"empty and duplicate keys", []string{"", "/w/alpha", "/w/alpha"}, nil, "",
			map[string]string{"": "", "/w/alpha": "alpha"}},
		// Directories without a repository are shown as paths, with the home
		// directory abbreviated to ~; they take no part in collision handling.
		{"directory under home", []string{"/w/alpha", "/home/u/notes/x"}, []string{"/home/u/notes/x"}, "/home/u",
			map[string]string{"/w/alpha": "alpha", "/home/u/notes/x": "~/notes/x"}},
		{"directory outside home keeps its path", []string{"/w/alpha", "/srv/data"}, []string{"/srv/data"}, "/home/u",
			map[string]string{"/w/alpha": "alpha", "/srv/data": "/srv/data"}},
		{"directory without home", []string{"/home/u/notes"}, []string{"/home/u/notes"}, "",
			map[string]string{"/home/u/notes": "/home/u/notes"}},
		{"home itself", []string{"/home/u"}, []string{"/home/u"}, "/home/u",
			map[string]string{"/home/u": "~"}},
		{"home as a prefix of another directory", []string{"/home/u", "/home/ubuntu/x"}, []string{"/home/u", "/home/ubuntu/x"}, "/home/u",
			map[string]string{"/home/u": "~", "/home/ubuntu/x": "/home/ubuntu/x"}},
		{"trailing slashes", []string{"/home/u/notes/"}, []string{"/home/u/notes/"}, "/home/u/",
			map[string]string{"/home/u/notes/": "~/notes"}},
		{"repo below home is not abbreviated", []string{"/home/u/w/alpha"}, nil, "/home/u",
			map[string]string{"/home/u/w/alpha": "alpha"}},
		{"directory does not collide with a repo of the same base name", []string{"/w/notes", "/home/u/notes"}, []string{"/home/u/notes"}, "/home/u",
			map[string]string{"/w/notes": "notes", "/home/u/notes": "~/notes"}},
		{"dir that is not a key is ignored", []string{"/w/alpha"}, []string{"/home/u/notes"}, "/home/u",
			map[string]string{"/w/alpha": "alpha"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Names(tt.keys, tt.dirs, tt.home); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Names(%q, %q, %q) = %v, want %v", tt.keys, tt.dirs, tt.home, got, tt.want)
			}
		})
	}
}

func TestTilde(t *testing.T) {
	tests := []struct {
		name, path, home, want string
	}{
		{"below home", "/home/u/notes/x", "/home/u", "~/notes/x"},
		{"home itself", "/home/u", "/home/u", "~"},
		{"home with trailing slash", "/home/u/notes", "/home/u/", "~/notes"},
		{"path with trailing slash", "/home/u/notes/", "/home/u", "~/notes"},
		{"prefix but not a parent", "/home/ubuntu/x", "/home/u", "/home/ubuntu/x"},
		{"outside home", "/srv/data", "/home/u", "/srv/data"},
		{"empty home", "/home/u/notes", "", "/home/u/notes"},
		{"root as home", "/x", "/", "/x"},
		{"relative path", "notes", "/home/u", "notes"},
		{"empty path", "", "/home/u", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Tilde(tt.path, tt.home); got != tt.want {
				t.Errorf("Tilde(%q, %q) = %q, want %q", tt.path, tt.home, got, tt.want)
			}
		})
	}
}

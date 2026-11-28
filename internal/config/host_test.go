package config

import (
	"errors"
	"strings"
	"testing"
)

// TestHost: the host defaults to the first label of the machine's host
// name in lower case, a config file overrides it, and a name that cannot
// be a file or directory name in a pool is an error.
func TestHost(t *testing.T) {
	for _, tc := range []struct {
		name, hostname string
		hostErr        error
		content        string
		want, wantErr  string
		origin         Origin
	}{
		{"default", "mara-nb", nil, "", "mara-nb", "", OriginDefault},
		{"default without domain", "MacBook-Pro.fritz.box", nil, "", "macbook-pro", "", OriginDefault},
		{"default unknown", "", errors.New("no name"), "", "", "", OriginDefault},
		{"default not a valid name", "bad name", nil, "", "", "", OriginDefault},
		{"file", "mara-nb", nil, "host = \"host-a\"\n", "host-a", "", OriginFile},
		{"file with dots", "x", nil, "host = \"build.box_2\"\n", "build.box_2", "", OriginFile},
		{"empty", "x", nil, "host = \"\"\n", "", "host", OriginFile},
		{"space", "x", nil, "host = \"host a\"\n", "", "host", OriginFile},
		{"slash", "x", nil, "host = \"a/b\"\n", "", "host", OriginFile},
		{"dot first", "x", nil, "host = \".hidden\"\n", "", "host", OriginFile},
		{"too long", "x", nil, "host = \"" + strings.Repeat("a", 64) + "\"\n", "", "host", OriginFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := Options{GOOS: "linux", Home: "/home/u", Getenv: env(nil),
				Hostname: func() (string, error) { return tc.hostname, tc.hostErr }}
			if tc.content != "" {
				opts.ConfigFile = writeFile(t, t.TempDir(), "c.toml", tc.content)
			}
			c, err := Load(opts)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want one about %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.Host != tc.want {
				t.Errorf("Host = %q, want %q", c.Host, tc.want)
			}
			if o := c.Origin("host"); o != tc.origin {
				t.Errorf("origin = %v, want %v", o, tc.origin)
			}
		})
	}
}

// TestPoolDir: [backup] pool_dir is off by default; a path from the file
// is expanded like backup.dir.
func TestPoolDir(t *testing.T) {
	for _, tc := range []struct {
		name, content, want string
	}{
		{"default", "", ""},
		{"file", "[backup]\npool_dir = \"~/pool\"\n", "/home/u/pool"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := Options{GOOS: "linux", Home: "/home/u", Getenv: env(nil), Hostname: func() (string, error) { return "h", nil }}
			if tc.content != "" {
				opts.ConfigFile = writeFile(t, t.TempDir(), "c.toml", tc.content)
			}
			c, err := Load(opts)
			if err != nil {
				t.Fatal(err)
			}
			if c.Backup.PoolDir != tc.want {
				t.Errorf("PoolDir = %q, want %q", c.Backup.PoolDir, tc.want)
			}
		})
	}
}

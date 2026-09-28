package paths

import "testing"

func fakeGetenv(m map[string]string) func(string) string {
	return func(k string) string {
		return m[k]
	}
}

func TestResolve_XDGOverrides(t *testing.T) {
	getenv := fakeGetenv(map[string]string{
		"HOME":            "/home/u",
		"XDG_CONFIG_HOME": "/custom/config",
		"XDG_DATA_HOME":   "/custom/data",
		"XDG_CACHE_HOME":  "/custom/cache",
	})

	p, err := Resolve(getenv)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Home != "/home/u" {
		t.Errorf("Home = %q, want %q", p.Home, "/home/u")
	}
	if p.ConfigDir != "/custom/config/jig" {
		t.Errorf("ConfigDir = %q, want %q", p.ConfigDir, "/custom/config/jig")
	}
	if p.DataDir != "/custom/data/jig" {
		t.Errorf("DataDir = %q, want %q", p.DataDir, "/custom/data/jig")
	}
	if p.CacheDir != "/custom/cache/jig" {
		t.Errorf("CacheDir = %q, want %q", p.CacheDir, "/custom/cache/jig")
	}
}

func TestResolve_DefaultsUnderHome(t *testing.T) {
	getenv := fakeGetenv(map[string]string{
		"HOME": "/home/u",
	})

	p, err := Resolve(getenv)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.ConfigDir != "/home/u/.config/jig" {
		t.Errorf("ConfigDir = %q, want %q", p.ConfigDir, "/home/u/.config/jig")
	}
	if p.DataDir != "/home/u/.local/share/jig" {
		t.Errorf("DataDir = %q, want %q", p.DataDir, "/home/u/.local/share/jig")
	}
	if p.CacheDir != "/home/u/.cache/jig" {
		t.Errorf("CacheDir = %q, want %q", p.CacheDir, "/home/u/.cache/jig")
	}
}

func TestResolve_StateDir(t *testing.T) {
	getenv := fakeGetenv(map[string]string{
		"HOME":           "/home/u",
		"XDG_STATE_HOME": "/s",
	})
	p, err := Resolve(getenv)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.StateDir != "/s/jig" {
		t.Errorf("StateDir = %q, want %q", p.StateDir, "/s/jig")
	}

	p, err = Resolve(fakeGetenv(map[string]string{"HOME": "/home/u"}))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.StateDir != "/home/u/.local/state/jig" {
		t.Errorf("StateDir = %q, want %q", p.StateDir, "/home/u/.local/state/jig")
	}
}

func TestResolve_MissingHomeAndXDGErrors(t *testing.T) {
	getenv := fakeGetenv(map[string]string{})

	if _, err := Resolve(getenv); err == nil {
		t.Fatal("Resolve: want error when HOME and all XDG vars are unset, got nil")
	}
}

func TestResolve_NoFilesystemIO(t *testing.T) {
	// Resolve must not touch the filesystem: a nonexistent HOME is fine.
	getenv := fakeGetenv(map[string]string{"HOME": "/does/not/exist"})
	p, err := Resolve(getenv)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.ConfigDir != "/does/not/exist/.config/jig" {
		t.Errorf("ConfigDir = %q, want %q", p.ConfigDir, "/does/not/exist/.config/jig")
	}
}

func TestExpandHome(t *testing.T) {
	cases := []struct {
		name, p, home, want string
	}{
		{"tilde-only", "~", "/home/u", "/home/u"},
		{"tilde-slash", "~/x", "/home/u", "/home/u/x"},
		{"tilde-slash-nested", "~/a/b", "/home/u", "/home/u/a/b"},
		{"tilde-user-unchanged", "~user/x", "/home/u", "~user/x"},
		{"absolute-unchanged", "/abs/path", "/home/u", "/abs/path"},
		{"relative-unchanged", "rel/path", "/home/u", "rel/path"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ExpandHome(c.p, c.home)
			if got != c.want {
				t.Errorf("ExpandHome(%q, %q) = %q, want %q", c.p, c.home, got, c.want)
			}
		})
	}
}

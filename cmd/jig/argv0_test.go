package main

import (
	"slices"
	"testing"
)

func TestBareArgv(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{"./bin/jig", "run", "hi"}, []string{"jig", "run", "hi"}},
		{[]string{"/usr/local/bin/jig"}, []string{"jig"}},
		{[]string{"jig", "run"}, nil},
		{nil, nil},
	}
	for _, c := range cases {
		if got := bareArgv(c.in); !slices.Equal(got, c.want) {
			t.Errorf("bareArgv(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

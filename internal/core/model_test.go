package core

import "testing"

func TestParseModelRef(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    ModelRef
		wantErr bool
	}{
		{
			name: "simple",
			in:   "anthropic/claude-haiku-4-5",
			want: ModelRef{Provider: "anthropic", Model: "claude-haiku-4-5"},
		},
		{
			name: "splits on first slash only",
			in:   "openrouter/anthropic/claude-sonnet-4",
			want: ModelRef{Provider: "openrouter", Model: "anthropic/claude-sonnet-4"},
		},
		{
			name:    "empty",
			in:      "",
			wantErr: true,
		},
		{
			name:    "no slash",
			in:      "noslash",
			wantErr: true,
		},
		{
			name:    "empty provider",
			in:      "/x",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseModelRef(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseModelRef(%q) = %v, nil, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseModelRef(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseModelRef(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestModelInfo_Cost(t *testing.T) {
	m := ModelInfo{
		CostIn:         3,
		CostOut:        15,
		CostCacheRead:  0.3,
		CostCacheWrite: 3.75,
	}
	u := Usage{Input: 1e6, Output: 1e6, CacheRead: 1e6, CacheWrite: 1e6}

	got := m.Cost(u)
	want := 22.05
	if got != want {
		t.Errorf("Cost() = %v, want %v", got, want)
	}
}

package sat

import "testing"

func TestToSimplified(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"愛體國", "爱体国"},
		{"這是繁體字", "这是繁体字"},
		{"English text 123", "English text 123"},
		{"简体字保持不变", "简体字保持不变"},
		{"妳是誰", "你是谁"},
	}

	for _, tt := range tests {
		got := ToSimplified(tt.in)
		if got != tt.want {
			t.Errorf("ToSimplified(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

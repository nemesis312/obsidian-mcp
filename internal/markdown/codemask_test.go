package markdown_test

import (
	"strings"
	"testing"

	"github.com/nemesis312/obsidian-mcp/internal/markdown"
)

func TestMaskCode(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "a [[X]] b", "a [[X]] b"},
		{"inline code", "`[[X]]` [[Y]]", "        [[Y]]"},
		{"fenced block", "x\n```\n[[X]]\n```\n[[Y]]", "x\n   \n     \n   \n[[Y]]"},
		{"tilde fence", "~~~\n[[X]]\n~~~", "   \n     \n   "},
		{"longer fence", "````\n```\n[[X]]\n````\n[[Y]]", "    \n   \n     \n    \n[[Y]]"},
		{"unclosed backtick", "` [[X]]", "` [[X]]"},
		{"unclosed fence", "```\n[[X]]", "   \n     "},
		{"crlf", "#a\r\n```\r\n#b\r\n```\r\n", "#a\r\n   \r\n  \r\n   \r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := markdown.MaskCode(tt.in)
			if len(got) != len(tt.in) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.in))
			}
			for i := 0; i < len(tt.in); i++ {
				if (tt.in[i] == '\n') != (got[i] == '\n') || (tt.in[i] == '\r') != (got[i] == '\r') {
					t.Fatalf("line break mismatch at %d: %q", i, got)
				}
			}
			if got != tt.want {
				t.Errorf("MaskCode(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if strings.Contains(tt.name, "fence") && strings.Contains(got, "[[X]]") {
				t.Errorf("code content leaked: %q", got)
			}
		})
	}
}

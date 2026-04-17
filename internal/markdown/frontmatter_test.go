package markdown

import (
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantData    bool
		wantBody    string
		wantErr     bool
	}{
		{
			name: "valid frontmatter",
			content: `---
title: Test Note
tags: [tag1, tag2]
---
Body content here`,
			wantData: true,
			wantBody: "Body content here",
			wantErr:  false,
		},
		{
			name:     "no frontmatter",
			content:  "Just body content",
			wantData: false,
			wantBody: "Just body content",
			wantErr:  false,
		},
		{
			name: "empty frontmatter",
			content: `---
---
Body content`,
			wantData: false,
			wantBody: "Body content",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, body, err := ParseFrontmatter(tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFrontmatter() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantData && fm == nil {
				t.Error("expected frontmatter data, got nil")
			}
			if body != tt.wantBody {
				t.Errorf("ParseFrontmatter() body = %v, want %v", body, tt.wantBody)
			}
		})
	}
}

func TestUpdateFrontmatter(t *testing.T) {
	content := `---
title: Old Title
---
Body content`

	updates := map[string]interface{}{
		"title":  "New Title",
		"author": "Test Author",
	}

	updated, err := UpdateFrontmatter(content, updates)
	if err != nil {
		t.Fatalf("UpdateFrontmatter() error = %v", err)
	}

	fm, body, err := ParseFrontmatter(updated)
	if err != nil {
		t.Fatalf("ParseFrontmatter() error = %v", err)
	}

	if fm.Data["title"] != "New Title" {
		t.Errorf("title = %v, want New Title", fm.Data["title"])
	}

	if fm.Data["author"] != "Test Author" {
		t.Errorf("author = %v, want Test Author", fm.Data["author"])
	}

	if body != "Body content" {
		t.Errorf("body changed: got %v, want Body content", body)
	}
}

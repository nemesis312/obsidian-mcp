package markdown

import (
	"reflect"
	"testing"
)

func TestParseInlineTags(t *testing.T) {
	content := `
Some text with #tag1 and #tag2/nested.
Also #another-tag here.
`

	tags := ParseInlineTags(content)

	expected := []string{"tag1", "tag2/nested", "another-tag"}
	if !reflect.DeepEqual(tags, expected) {
		t.Errorf("ParseInlineTags() = %v, want %v", tags, expected)
	}
}

func TestParseFrontmatterTags(t *testing.T) {
	fmData := map[string]interface{}{
		"tags": []interface{}{"tag1", "tag2"},
	}

	tags := ParseFrontmatterTags(fmData)

	expected := []string{"tag1", "tag2"}
	if !reflect.DeepEqual(tags, expected) {
		t.Errorf("ParseFrontmatterTags() = %v, want %v", tags, expected)
	}
}

func TestRenameTag(t *testing.T) {
	content := `Some text with #oldtag and other content.
Another line with #oldtag here.`

	result := RenameTag(content, "oldtag", "newtag")

	tags := ParseInlineTags(result)

	hasNewTag := false
	hasOldTag := false

	for _, tag := range tags {
		if tag == "newtag" {
			hasNewTag = true
		}
		if tag == "oldtag" {
			hasOldTag = true
		}
	}

	if !hasNewTag {
		t.Error("result should contain #newtag")
	}

	if hasOldTag {
		t.Error("result should not contain #oldtag")
	}
}

package markdown

import (
	"reflect"
	"testing"
)

func TestParseWikilinks(t *testing.T) {
	content := `
Some text with [[Page 1]] and [[Page 2|Alias]] links.
Also [[Page 3#Section]] and [[Page 4#Section|Custom Alias]].
`

	links := ParseWikilinks(content)

	if len(links) != 4 {
		t.Errorf("expected 4 links, got %d", len(links))
	}

	// Check first link
	if links[0].Target != "Page 1" {
		t.Errorf("link[0].Target = %v, want Page 1", links[0].Target)
	}

	// Check second link with alias
	if links[1].Target != "Page 2" || links[1].Alias != "Alias" {
		t.Errorf("link[1] = %+v, want Target=Page 2, Alias=Alias", links[1])
	}

	// Check third link with heading
	if links[2].Target != "Page 3" || links[2].Heading != "Section" {
		t.Errorf("link[2] = %+v, want Target=Page 3, Heading=Section", links[2])
	}
}

func TestGetOutgoingLinks(t *testing.T) {
	content := `
[[Page 1]]
[[Page 2]]
[[Page 1]]
`

	links := GetOutgoingLinks(content)

	expected := []string{"Page 1", "Page 2"}
	if !reflect.DeepEqual(links, expected) {
		t.Errorf("GetOutgoingLinks() = %v, want %v", links, expected)
	}
}

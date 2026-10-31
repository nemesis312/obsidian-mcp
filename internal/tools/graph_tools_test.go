package tools_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGraphToolsJSON(t *testing.T) {
	e := newEnv(t)
	e.write("A.md", "[[B]] ![[img.png]] [[Missing]]")
	e.write("sub/B.md", "[x](../A.md)")
	e.write("img.png", "p")
	e.write("Lonely.md", "")
	e.write("x/Dup.md", "")
	e.write("y/Dup.md", "[[A]]")
	e.write("Amb.md", "[[Dup]]")

	noBackslash := func(tool, text string) {
		t.Helper()
		if strings.Contains(text, `\\`) {
			t.Errorf("%s output contains a backslash: %s", tool, text)
		}
		if strings.Contains(text, e.root) {
			t.Errorf("%s output contains the vault root: %s", tool, text)
		}
		if strings.Contains(text, "\n  ") {
			t.Errorf("%s output is indented, want compact: %s", tool, text)
		}
	}

	// get_backlinks
	text, isErr := e.call("get_backlinks", map[string]any{"path": "SUB/b"})
	if isErr {
		t.Fatalf("get_backlinks: %s", text)
	}
	noBackslash("get_backlinks", text)
	var bl struct {
		Path      string `json:"path"`
		Backlinks []struct {
			From      string `json:"from"`
			Kind      string `json:"kind"`
			Ambiguous *bool  `json:"ambiguous"`
		} `json:"backlinks"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(text), &bl); err != nil {
		t.Fatalf("unmarshal %s: %v", text, err)
	}
	if bl.Path != "sub/B.md" || bl.Count != 1 || len(bl.Backlinks) != 1 ||
		bl.Backlinks[0].From != "A.md" || bl.Backlinks[0].Kind != "wikilink" ||
		bl.Backlinks[0].Ambiguous == nil || *bl.Backlinks[0].Ambiguous {
		t.Errorf("get_backlinks = %s", text)
	}
	text, _ = e.call("get_backlinks", map[string]any{"path": "x/Dup.md"})
	if !strings.Contains(text, `{"from":"Amb.md","kind":"wikilink","ambiguous":true}`) {
		t.Errorf("get_backlinks(x/Dup.md) = %s, want an ambiguous backlink from Amb.md", text)
	}

	// get_outgoing_links
	text, isErr = e.call("get_outgoing_links", map[string]any{"path": "a"})
	if isErr {
		t.Fatalf("get_outgoing_links: %s", text)
	}
	noBackslash("get_outgoing_links", text)
	var out struct {
		Path  string `json:"path"`
		Links []struct {
			To        string `json:"to"`
			ToKind    string `json:"to_kind"`
			Kind      string `json:"kind"`
			Ambiguous *bool  `json:"ambiguous"`
		} `json:"links"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal %s: %v", text, err)
	}
	if out.Path != "A.md" || out.Count != 3 || len(out.Links) != 3 {
		t.Fatalf("get_outgoing_links = %s", text)
	}
	got := map[string]string{}
	for _, l := range out.Links {
		if l.Ambiguous == nil {
			t.Errorf("link %s has no ambiguous field", l.To)
		}
		got[l.To] = l.ToKind + "/" + l.Kind
	}
	if got["sub/B.md"] != "note/wikilink" || got["img.png"] != "attachment/embed" || got["missing"] != "ghost/wikilink" {
		t.Errorf("get_outgoing_links kinds = %v", got)
	}

	// markdown kind
	text, _ = e.call("get_outgoing_links", map[string]any{"path": "sub/B.md"})
	if !strings.Contains(text, `"kind":"markdown"`) || !strings.Contains(text, `"to":"A.md"`) {
		t.Errorf("markdown link output = %s", text)
	}

	// get_orphaned_notes
	text, isErr = e.call("get_orphaned_notes", map[string]any{})
	if isErr {
		t.Fatalf("get_orphaned_notes: %s", text)
	}
	noBackslash("get_orphaned_notes", text)
	var orph struct {
		Orphans []string `json:"orphans"`
		Count   int      `json:"count"`
	}
	if err := json.Unmarshal([]byte(text), &orph); err != nil {
		t.Fatalf("unmarshal %s: %v", text, err)
	}
	if orph.Count != 1 || len(orph.Orphans) != 1 || orph.Orphans[0] != "Lonely.md" {
		t.Errorf("get_orphaned_notes = %s", text)
	}

	// get_link_graph
	text, isErr = e.call("get_link_graph", map[string]any{})
	if isErr {
		t.Fatalf("get_link_graph: %s", text)
	}
	noBackslash("get_link_graph", text)
	var g struct {
		Nodes []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"nodes"`
		Edges []struct {
			From      string `json:"from"`
			To        string `json:"to"`
			ToKind    string `json:"to_kind"`
			Kind      string `json:"kind"`
			Ambiguous *bool  `json:"ambiguous"`
		} `json:"edges"`
		NodeCount int `json:"node_count"`
		EdgeCount int `json:"edge_count"`
	}
	if err := json.Unmarshal([]byte(text), &g); err != nil {
		t.Fatalf("unmarshal %s: %v", text, err)
	}
	if g.NodeCount != len(g.Nodes) || g.EdgeCount != len(g.Edges) || g.NodeCount != 8 || g.EdgeCount != 6 {
		t.Errorf("get_link_graph counts = %s", text)
	}
	ghost, amb := false, false
	for _, ed := range g.Edges {
		if ed.Ambiguous == nil {
			t.Errorf("edge %s -> %s has no ambiguous field", ed.From, ed.To)
		} else if *ed.Ambiguous {
			amb = ed.From == "Amb.md" && ed.To == "x/Dup.md"
		}
		if ed.ToKind == "ghost" && ed.To == "missing" && ed.From == "A.md" && ed.Kind == "wikilink" {
			ghost = true
		}
	}
	if !ghost {
		t.Errorf("get_link_graph has no ghost edge: %s", text)
	}
	if !amb {
		t.Errorf("get_link_graph has no ambiguous edge Amb.md -> x/Dup.md: %s", text)
	}
	if desc := e.s.GetTool("get_link_graph").Tool.Description; !strings.Contains(desc, "Full graph; can be large on big vaults.") {
		t.Errorf("get_link_graph description = %q", desc)
	}
}

func TestGraphToolsNotFound(t *testing.T) {
	e := newEnv(t)
	e.write("A.md", "x")
	for _, tool := range []string{"get_backlinks", "get_outgoing_links"} {
		for _, arg := range []string{"nope", "../x"} {
			text, isErr := e.call(tool, map[string]any{"path": arg})
			if !isErr {
				t.Fatalf("%s(%q): want error, got %s", tool, arg, text)
			}
			if want := "note not found: " + arg; text != want {
				t.Errorf("%s(%q) = %q, want %q", tool, arg, text, want)
			}
			if strings.Contains(text, e.root) {
				t.Errorf("%s(%q) leaks root: %q", tool, arg, text)
			}
		}
	}
}

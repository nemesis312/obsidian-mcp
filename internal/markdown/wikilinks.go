package markdown

import (
	"regexp"
	"strings"
)

// Wikilink patterns:
// [[Page Name]]
// [[Page Name|Alias]]
// [[Page Name#Heading]]
// [[Page Name#Heading|Alias]]

var wikilinkRegex = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// Wikilink represents an Obsidian internal link
type Wikilink struct {
	Raw     string // Full [[...]] text
	Target  string // Page name
	Heading string // Section heading (if any)
	Alias   string // Display alias (if any)
}

// ParseWikilinks extracts all wikilinks from markdown content
func ParseWikilinks(content string) []Wikilink {
	matches := wikilinkRegex.FindAllStringSubmatch(content, -1)
	var links []Wikilink

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		raw := match[0]
		inner := match[1]

		link := Wikilink{Raw: raw}

		// Split on | for alias
		parts := strings.SplitN(inner, "|", 2)
		target := parts[0]
		if len(parts) == 2 {
			link.Alias = strings.TrimSpace(parts[1])
		}

		// Split on # for heading
		headingParts := strings.SplitN(target, "#", 2)
		link.Target = strings.TrimSpace(headingParts[0])
		if len(headingParts) == 2 {
			link.Heading = strings.TrimSpace(headingParts[1])
		}

		links = append(links, link)
	}

	return links
}

// GetOutgoingLinks returns unique target page names from wikilinks
func GetOutgoingLinks(content string) []string {
	links := ParseWikilinks(content)
	seen := make(map[string]bool)
	var targets []string

	for _, link := range links {
		if link.Target == "" {
			continue
		}
		if !seen[link.Target] {
			seen[link.Target] = true
			targets = append(targets, link.Target)
		}
	}

	return targets
}

// ReplaceWikilink replaces all instances of a wikilink target
func ReplaceWikilink(content, oldTarget, newTarget string) string {
	links := ParseWikilinks(content)
	result := content

	for _, link := range links {
		if link.Target == oldTarget {
			newInner := newTarget
			if link.Heading != "" {
				newInner += "#" + link.Heading
			}
			if link.Alias != "" {
				newInner += "|" + link.Alias
			}
			newLink := "[[" + newInner + "]]"
			result = strings.ReplaceAll(result, link.Raw, newLink)
		}
	}

	return result
}

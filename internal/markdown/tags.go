package markdown

import (
	"regexp"
	"strings"
)

// Tag patterns:
// #tag
// #nested/tag
// frontmatter: tags: [tag1, tag2]

var inlineTagRegex = regexp.MustCompile(`(?:^|\s)#([a-zA-Z0-9/_-]+)`)

// ParseInlineTags extracts inline tags from markdown content
func ParseInlineTags(content string) []string {
	matches := inlineTagRegex.FindAllStringSubmatch(content, -1)
	var tags []string

	for _, match := range matches {
		if len(match) >= 2 {
			tags = append(tags, match[1])
		}
	}

	return uniqueTags(tags)
}

// ParseFrontmatterTags extracts tags from frontmatter data
func ParseFrontmatterTags(fmData map[string]interface{}) []string {
	if fmData == nil {
		return nil
	}

	var tags []string

	// Check "tags" field (can be array or single string)
	if val, ok := fmData["tags"]; ok {
		tags = append(tags, extractTagValues(val)...)
	}

	// Check "tag" field (alternate convention)
	if val, ok := fmData["tag"]; ok {
		tags = append(tags, extractTagValues(val)...)
	}

	return uniqueTags(tags)
}

// ParseAllTags extracts both inline and frontmatter tags
func ParseAllTags(content string, fmData map[string]interface{}) []string {
	inline := ParseInlineTags(content)
	frontmatter := ParseFrontmatterTags(fmData)

	all := append(inline, frontmatter...)
	return uniqueTags(all)
}

// RenameTag replaces all instances of a tag with a new name
func RenameTag(content string, oldTag, newTag string) string {
	// Replace inline tags
	newPattern := "#" + newTag

	// Use word boundary to avoid partial matches
	lines := strings.Split(content, "\n")

	for i, line := range lines {
		// Match whole tag only
		re := regexp.MustCompile(`(^|\s)#` + regexp.QuoteMeta(oldTag) + `(\s|$)`)
		lines[i] = re.ReplaceAllString(line, "${1}"+newPattern+"${2}")
	}

	result := strings.Join(lines, "\n")
	return result
}

// extractTagValues handles various YAML tag formats
func extractTagValues(val interface{}) []string {
	var tags []string

	switch v := val.(type) {
	case string:
		// Single string tag
		tags = append(tags, strings.TrimSpace(v))
	case []interface{}:
		// Array of tags
		for _, item := range v {
			if str, ok := item.(string); ok {
				tags = append(tags, strings.TrimSpace(str))
			}
		}
	case []string:
		// String array
		for _, str := range v {
			tags = append(tags, strings.TrimSpace(str))
		}
	}

	return tags
}

// uniqueTags removes duplicates
func uniqueTags(tags []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		tag = strings.TrimPrefix(tag, "#") // Normalize
		if tag != "" && !seen[tag] {
			seen[tag] = true
			result = append(result, tag)
		}
	}

	return result
}

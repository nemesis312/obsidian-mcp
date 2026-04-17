package vault

import (
	"obsidian-mcp/internal/markdown"
)

// TagIndex maps tag names to file paths
type TagIndex map[string][]string

// ListAllTags returns all unique tags in the vault
func (v *Vault) ListAllTags() ([]string, error) {
	files, err := v.ListFiles()
	if err != nil {
		return nil, err
	}

	tagSet := make(map[string]bool)

	for _, file := range files {
		content, err := v.ReadNote(file)
		if err != nil {
			continue
		}

		// Parse frontmatter for tags
		fm, body, err := markdown.ParseFrontmatter(content)
		if err != nil {
			continue
		}

		var fmData map[string]interface{}
		if fm != nil {
			fmData = fm.Data
		}

		// Get all tags
		tags := markdown.ParseAllTags(body, fmData)
		for _, tag := range tags {
			tagSet[tag] = true
		}
	}

	// Convert to sorted list
	var tags []string
	for tag := range tagSet {
		tags = append(tags, tag)
	}

	return tags, nil
}

// GetNotesByTag returns all notes containing a specific tag
func (v *Vault) GetNotesByTag(tag string) ([]string, error) {
	files, err := v.ListFiles()
	if err != nil {
		return nil, err
	}

	var matches []string

	for _, file := range files {
		content, err := v.ReadNote(file)
		if err != nil {
			continue
		}

		// Parse frontmatter for tags
		fm, body, err := markdown.ParseFrontmatter(content)
		if err != nil {
			continue
		}

		var fmData map[string]interface{}
		if fm != nil {
			fmData = fm.Data
		}

		// Check if tag exists
		tags := markdown.ParseAllTags(body, fmData)
		for _, t := range tags {
			if t == tag {
				matches = append(matches, file)
				break
			}
		}
	}

	return matches, nil
}

// RenameTag renames a tag across all notes in the vault
func (v *Vault) RenameTag(oldTag, newTag string) (int, error) {
	files, err := v.ListFiles()
	if err != nil {
		return 0, err
	}

	count := 0

	for _, file := range files {
		content, err := v.ReadNote(file)
		if err != nil {
			continue
		}

		// Parse frontmatter
		fm, body, err := markdown.ParseFrontmatter(content)
		if err != nil {
			continue
		}

		modified := false

		// Rename inline tags in body
		newBody := markdown.RenameTag(body, oldTag, newTag)
		if newBody != body {
			body = newBody
			modified = true
		}

		// Rename frontmatter tags
		if fm != nil && fm.Data != nil {
			fmTags := markdown.ParseFrontmatterTags(fm.Data)
			for i, tag := range fmTags {
				if tag == oldTag {
					fmTags[i] = newTag
					modified = true
				}
			}

			if modified {
				// Update frontmatter tags field
				fm.Data["tags"] = fmTags
			}
		}

		// Write back if modified
		if modified {
			var newContent string
			if fm != nil {
				newContent, err = markdown.UpdateFrontmatter(body, fm.Data)
				if err != nil {
					continue
				}
			} else {
				newContent = body
			}

			if err := v.WriteNote(file, newContent); err != nil {
				continue
			}

			count++
		}
	}

	return count, nil
}

// BuildTagIndex creates an index of all tags and their notes
func (v *Vault) BuildTagIndex() (TagIndex, error) {
	files, err := v.ListFiles()
	if err != nil {
		return nil, err
	}

	index := make(TagIndex)

	for _, file := range files {
		content, err := v.ReadNote(file)
		if err != nil {
			continue
		}

		fm, body, err := markdown.ParseFrontmatter(content)
		if err != nil {
			continue
		}

		var fmData map[string]interface{}
		if fm != nil {
			fmData = fm.Data
		}

		tags := markdown.ParseAllTags(body, fmData)
		for _, tag := range tags {
			index[tag] = append(index[tag], file)
		}
	}

	return index, nil
}

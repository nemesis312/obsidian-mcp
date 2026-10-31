package vault

import (
	"fmt"

	"github.com/nemesis312/obsidian-mcp/internal/markdown"
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

// RenameFailure describes a note that could not be processed by RenameTag.
type RenameFailure struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// RenameResult reports which notes were rewritten and which failed.
type RenameResult struct {
	Renamed []string        `json:"renamed"`
	Failed  []RenameFailure `json:"failed"`
}

// RenameTag renames a tag (and its nested children, a -> new, a/b -> new/b)
// across all notes. Both names are validated; per-note errors are collected in
// the result instead of aborting or being silently dropped.
func (v *Vault) RenameTag(oldTag, newTag string) (RenameResult, error) {
	res := RenameResult{Renamed: []string{}, Failed: []RenameFailure{}}

	oldTag, err := markdown.NormalizeTagName(oldTag)
	if err != nil {
		return res, fmt.Errorf("invalid old_tag: %w", err)
	}
	newTag, err = markdown.NormalizeTagName(newTag)
	if err != nil {
		return res, fmt.Errorf("invalid new_tag: %w", err)
	}
	if oldTag == newTag {
		return res, fmt.Errorf("old_tag and new_tag are identical")
	}

	files, err := v.ListFiles()
	if err != nil {
		return res, err
	}

	renamer := markdown.NewTagRenamer(oldTag, newTag) // compiled patterns are shared; built once per rename

	for _, file := range files {
		// An unlocked read skips notes without the tag; a note that has it
		// is re-read, rewritten and written under the guard, which is held
		// for that one note only.
		content, err := v.ReadNote(file)
		if err != nil {
			res.Failed = append(res.Failed, RenameFailure{Path: file, Error: err.Error()})
			continue
		}
		if _, changed, err := renamer.RenameNote(content); err != nil {
			res.Failed = append(res.Failed, RenameFailure{Path: file, Error: err.Error()})
			continue
		} else if !changed {
			continue
		}

		written, err := v.renameTagInNote(renamer, file)
		if err != nil {
			res.Failed = append(res.Failed, RenameFailure{Path: file, Error: err.Error()})
			continue
		}
		if written {
			v.indexUpdate(file)
			res.Renamed = append(res.Renamed, file)
		}
	}

	return res, nil
}

// renameTagInNote rewrites the tag in one note under the write guard,
// re-reading it from disk first. It reports whether the note was written.
func (v *Vault) renameTagInNote(renamer *markdown.TagRenamer, file string) (bool, error) {
	unlock, err := v.guard.lock()
	if err != nil {
		return false, err
	}
	defer unlock()
	content, err := v.ReadNote(file)
	if err != nil {
		return false, err
	}
	updated, changed, err := renamer.RenameNote(content)
	if err != nil || !changed {
		return false, err
	}
	if err := v.writeNoteLocked(file, updated); err != nil {
		return false, err
	}
	return true, nil
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

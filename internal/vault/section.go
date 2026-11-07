package vault

import (
	"errors"

	"github.com/nemesis312/obsidian-mcp/internal/markdown"
)

// SectionMode says how EditSection changes a section.
type SectionMode string

const (
	SectionReplace SectionMode = "replace"
	SectionAppend  SectionMode = "append"
	SectionPrepend SectionMode = "prepend"
)

// ErrSectionVersionRequired is returned when a section replace has no expected version.
var ErrSectionVersionRequired = errors.New(`expected_version is required to replace a section; read it with get_note and pass its "version"`)

// EditSection changes the section of note arg that heading names (its
// subsections included) and leaves every other byte alone. expected is
// required for SectionReplace and optional for append and prepend. The
// heading is resolved against the content read under the write guard.
func (v *Vault) EditSection(arg, heading string, mode SectionMode, content, expected string) (NoteSnapshot, error) {
	switch mode {
	case SectionReplace, SectionAppend, SectionPrepend:
	default:
		return NoteSnapshot{}, errors.New("invalid mode: use replace, append or prepend")
	}
	rel, err := v.addressForWrite(arg)
	if err != nil {
		return NoteSnapshot{}, err
	}
	if mode == SectionReplace && expected == "" {
		return NoteSnapshot{}, ErrSectionVersionRequired
	}
	if expected != "" {
		if err := checkVersion(expected); err != nil {
			return NoteSnapshot{}, err
		}
	}
	return v.modifyNote(arg, rel, expected, func(current string) (string, error) {
		h, err := markdown.FindSection(current, heading)
		if err != nil {
			return "", err
		}
		switch mode {
		case SectionReplace:
			return markdown.ReplaceSection(current, h, content), nil
		case SectionAppend:
			return markdown.AppendToSection(current, h, content), nil
		default:
			return markdown.PrependToSection(current, h, content), nil
		}
	})
}

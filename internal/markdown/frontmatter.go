package markdown

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const frontmatterDelimiter = "---"

// Frontmatter represents YAML metadata at the start of a note
type Frontmatter struct {
	Data map[string]interface{}
	Raw  string
}

// ParseFrontmatter extracts YAML frontmatter from markdown content
func ParseFrontmatter(content string) (*Frontmatter, string, error) {
	lines := strings.Split(content, "\n")

	// Must start with ---
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != frontmatterDelimiter {
		return nil, content, nil // No frontmatter
	}

	// Find closing ---
	endIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == frontmatterDelimiter {
			endIdx = i
			break
		}
	}

	if endIdx == -1 {
		return nil, content, fmt.Errorf("unclosed frontmatter delimiter")
	}

	// Extract frontmatter and body
	fmLines := lines[1:endIdx]
	fmRaw := strings.Join(fmLines, "\n")

	var body string
	if endIdx+1 < len(lines) {
		body = strings.Join(lines[endIdx+1:], "\n")
	}

	// Parse YAML
	var data map[string]interface{}
	if len(fmRaw) > 0 {
		if err := yaml.Unmarshal([]byte(fmRaw), &data); err != nil {
			return nil, content, fmt.Errorf("invalid frontmatter YAML: %w", err)
		}
	}

	fm := &Frontmatter{
		Data: data,
		Raw:  fmRaw,
	}

	return fm, body, nil
}

// UpdateFrontmatter replaces frontmatter in markdown while preserving body
func UpdateFrontmatter(content string, updates map[string]interface{}) (string, error) {
	fm, body, err := ParseFrontmatter(content)
	if err != nil {
		return "", err
	}

	// Merge updates
	if fm == nil {
		fm = &Frontmatter{Data: make(map[string]interface{})}
	}
	if fm.Data == nil {
		fm.Data = make(map[string]interface{})
	}

	for k, v := range updates {
		if v == nil {
			delete(fm.Data, k)
		} else {
			fm.Data[k] = v
		}
	}

	// Serialize updated frontmatter
	var yamlBuf bytes.Buffer
	encoder := yaml.NewEncoder(&yamlBuf)
	encoder.SetIndent(2)

	if err := encoder.Encode(fm.Data); err != nil {
		return "", fmt.Errorf("failed to encode frontmatter: %w", err)
	}
	encoder.Close()

	yamlStr := strings.TrimSpace(yamlBuf.String())

	// Reconstruct note
	var result strings.Builder
	result.WriteString(frontmatterDelimiter + "\n")
	result.WriteString(yamlStr + "\n")
	result.WriteString(frontmatterDelimiter + "\n")
	result.WriteString(body)

	return result.String(), nil
}

// HasFrontmatter checks if content has frontmatter
func HasFrontmatter(content string) bool {
	lines := strings.Split(content, "\n")
	return len(lines) >= 3 && strings.TrimSpace(lines[0]) == frontmatterDelimiter
}

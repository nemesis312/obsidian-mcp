package markdown

import (
	"encoding/json"
	"fmt"
)

// Canvas represents an Obsidian canvas file (.canvas)
type Canvas struct {
	Nodes []CanvasNode `json:"nodes"`
	Edges []CanvasEdge `json:"edges"`
}

// CanvasNode represents a node in a canvas
type CanvasNode struct {
	ID     string      `json:"id"`
	Type   string      `json:"type"` // "text", "file", "link", "group"
	X      float64     `json:"x"`
	Y      float64     `json:"y"`
	Width  float64     `json:"width"`
	Height float64     `json:"height"`
	Color  string      `json:"color,omitempty"`
	Text   string      `json:"text,omitempty"`   // For text nodes
	File   string      `json:"file,omitempty"`   // For file nodes
	URL    string      `json:"url,omitempty"`    // For link nodes
	Label  string      `json:"label,omitempty"`  // For groups
}

// CanvasEdge represents an edge connecting nodes
type CanvasEdge struct {
	ID       string `json:"id"`
	FromNode string `json:"fromNode"`
	ToNode   string `json:"toNode"`
	FromSide string `json:"fromSide,omitempty"` // "top", "right", "bottom", "left"
	ToSide   string `json:"toSide,omitempty"`
	Color    string `json:"color,omitempty"`
	Label    string `json:"label,omitempty"`
}

// ParseCanvas decodes a canvas JSON file
func ParseCanvas(content []byte) (*Canvas, error) {
	var canvas Canvas
	if err := json.Unmarshal(content, &canvas); err != nil {
		return nil, fmt.Errorf("invalid canvas JSON: %w", err)
	}
	return &canvas, nil
}

// GetCanvasFiles returns all file references in a canvas
func GetCanvasFiles(canvas *Canvas) []string {
	var files []string
	for _, node := range canvas.Nodes {
		if node.Type == "file" && node.File != "" {
			files = append(files, node.File)
		}
	}
	return files
}

// GetCanvasLinks returns all URL links in a canvas
func GetCanvasLinks(canvas *Canvas) []string {
	var links []string
	for _, node := range canvas.Nodes {
		if node.Type == "link" && node.URL != "" {
			links = append(links, node.URL)
		}
	}
	return links
}

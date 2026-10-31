package markdown

import "strings"

// MaskCode returns content with every byte inside a fenced code block
// (including the fence lines) or an inline code span (including the backticks)
// replaced by a space. '\n' and '\r' are kept, so the result has the same
// length and the same line structure as the input.
func MaskCode(content string) string {
	b := []byte(content)
	for _, r := range codeRanges(content) {
		for i := r[0]; i < r[1]; i++ {
			if b[i] != '\n' && b[i] != '\r' {
				b[i] = ' '
			}
		}
	}
	return string(b)
}

// codeRanges returns the sorted, non-overlapping byte ranges [start, end) of
// the code regions in content: fenced blocks (fence lines included, up to the
// end of content when unclosed) and inline code spans (backticks included).
// Ranges never include the '\n' that ends a line.
func codeRanges(content string) [][2]int {
	var ranges [][2]int
	var fenceChar byte
	var fenceLen int
	fenceStart := 0
	pos := 0 // start offset of the current line
	for pos <= len(content) {
		end := strings.IndexByte(content[pos:], '\n')
		if end < 0 {
			end = len(content)
		} else {
			end += pos
		}
		line := content[pos:end]
		stripped := strings.TrimLeft(line, " ")
		indent := len(line) - len(stripped)
		trimmed := strings.TrimRight(stripped, "\r")
		switch {
		case fenceLen > 0:
			if indent < 4 && isClosingFence(trimmed, fenceChar, fenceLen) {
				ranges = append(ranges, [2]int{fenceStart, end})
				fenceLen = 0
			}
		default:
			if c, n := openingFence(trimmed); indent < 4 && n >= 3 {
				fenceChar, fenceLen, fenceStart = c, n, pos
			} else {
				for _, r := range inlineCodeRanges(line) {
					ranges = append(ranges, [2]int{pos + r[0], pos + r[1]})
				}
			}
		}
		pos = end + 1
	}
	if fenceLen > 0 {
		ranges = append(ranges, [2]int{fenceStart, len(content)})
	}
	return ranges
}

func openingFence(s string) (byte, int) {
	if s == "" || (s[0] != '`' && s[0] != '~') {
		return 0, 0
	}
	c := s[0]
	n := 0
	for n < len(s) && s[n] == c {
		n++
	}
	if c == '`' && strings.Contains(s[n:], "`") {
		return 0, 0 // backtick info strings cannot contain backticks: inline code
	}
	return c, n
}

func isClosingFence(s string, c byte, minLen int) bool {
	n := 0
	for n < len(s) && s[n] == c {
		n++
	}
	return n >= minLen && strings.TrimSpace(s[n:]) == ""
}

// inlineCodeRanges returns the byte ranges of inline code spans in one line.
// A span opens with a run of n backticks and closes with the next run of
// exactly n backticks; an unmatched run is literal text.
//
// It is linear in the line length: the runs are collected once and each is
// linked to the next run of the same length, so an opener with no closer is
// known at once instead of rescanning the rest of the line.
func inlineCodeRanges(line string) [][2]int {
	if !strings.Contains(line, "`") {
		return nil
	}
	type run struct{ start, end int }
	var runs []run
	for i := 0; i < len(line); {
		if line[i] != '`' {
			i++
			continue
		}
		j := i
		for j < len(line) && line[j] == '`' {
			j++
		}
		runs = append(runs, run{i, j})
		i = j
	}
	next := make([]int, len(runs)) // index of the next run of the same length, or -1
	last := map[int]int{}
	for i := len(runs) - 1; i >= 0; i-- {
		n := runs[i].end - runs[i].start
		if k, ok := last[n]; ok {
			next[i] = k
		} else {
			next[i] = -1
		}
		last[n] = i
	}
	var ranges [][2]int
	for i := 0; i < len(runs); {
		k := next[i]
		if k < 0 {
			i++
			continue
		}
		ranges = append(ranges, [2]int{runs[i].start, runs[k].end})
		i = k + 1
	}
	return ranges
}

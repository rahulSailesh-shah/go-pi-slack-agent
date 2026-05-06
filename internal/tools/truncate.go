package tools

import (
	"fmt"
	"strings"
)

const (
	DefaultMaxBytes = 50 * 1024
	DefaultMaxLines = 100
)

type TruncationResult struct {
	Truncated         bool
	OutputLines       int
	OutputBytes       int
	TotalLines        int
	TotalBytes        int
	FirstLineExceeded bool
	Reason            string
}

func TruncateHead(text string) (string, TruncationResult) {
	return truncate(text, true)
}

func TruncateTail(text string) (string, TruncationResult) {
	return truncate(text, false)
}

func truncate(text string, head bool) (string, TruncationResult) {
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var r TruncationResult
	r.TotalLines = len(lines)
	r.TotalBytes = len(text)

	if head && len(lines) > 0 && len(lines[0]) > DefaultMaxBytes {
		r.Truncated = true
		r.FirstLineExceeded = true
		r.Reason = "first line exceeds 50KB"
		return "", r
	}

	src := lines
	if !head {
		rev := make([]string, len(lines))
		for i, l := range lines {
			rev[len(lines)-1-i] = l
		}
		src = rev
	}

	var selected []string
	byteCount := 0
	for _, line := range src {
		lineBytes := len(line) + 1
		if len(selected) >= DefaultMaxLines || byteCount+lineBytes > DefaultMaxBytes {
			r.Truncated = true
			if len(selected) >= DefaultMaxLines {
				r.Reason = fmt.Sprintf("exceeded %d lines", DefaultMaxLines)
			} else {
				r.Reason = fmt.Sprintf("exceeded %s", formatSize(DefaultMaxBytes))
			}
			break
		}
		selected = append(selected, line)
		byteCount += lineBytes
	}

	if !head {
		for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
			selected[i], selected[j] = selected[j], selected[i]
		}
	}

	r.OutputLines = len(selected)
	r.OutputBytes = byteCount
	return strings.Join(selected, "\n"), r
}

func formatSize(b int) string {
	const kb = 1024
	const mb = 1024 * 1024
	switch {
	case b >= mb:
		return fmt.Sprintf("%.1fMB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%dKB", b/kb)
	default:
		return fmt.Sprintf("%dB", b)
	}
}

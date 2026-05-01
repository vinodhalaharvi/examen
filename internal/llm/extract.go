package llm

import (
	"regexp"
	"strings"
)

// ExtractJSON pulls a JSON object or array out of text that might contain
// markdown code fences, prose preambles, or trailing commentary. Returns the
// raw JSON substring, or the input unchanged if nothing matches.
//
// Handles cases the LLM commonly emits:
//   - "Here is the problem: {...}"
//   - "```json\n{...}\n```"
//   - "```\n{...}\n```"
//   - Plain JSON
func ExtractJSON(s string) string {
	s = strings.TrimSpace(s)

	// Try fenced code block first
	if m := fenceRE.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}

	// Find the first { or [ and walk to the matching close, tracking depth
	// and respecting string literals (so braces inside strings don't confuse us).
	start := -1
	var openCh, closeCh byte
	for i := 0; i < len(s); i++ {
		if s[i] == '{' {
			start = i
			openCh, closeCh = '{', '}'
			break
		}
		if s[i] == '[' {
			start = i
			openCh, closeCh = '[', ']'
			break
		}
	}
	if start < 0 {
		return s
	}

	depth := 0
	inStr := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' && inStr {
			escape = true
			continue
		}
		if c == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		if c == openCh {
			depth++
		} else if c == closeCh {
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s[start:] // unbalanced; return what we found and let parser report
}

var fenceRE = regexp.MustCompile("(?s)```(?:json)?\\s*\\n?(.*?)\\n?```")

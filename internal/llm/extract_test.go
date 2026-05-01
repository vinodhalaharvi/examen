package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // expected substring of the output
	}{
		{
			name: "plain object",
			in:   `{"a":1}`,
			want: `{"a":1}`,
		},
		{
			name: "plain array",
			in:   `[{"a":1}]`,
			want: `[{"a":1}]`,
		},
		{
			name: "fenced json block",
			in:   "```json\n{\"a\":1}\n```",
			want: `{"a":1}`,
		},
		{
			name: "fenced unmarked block",
			in:   "```\n{\"a\":1}\n```",
			want: `{"a":1}`,
		},
		{
			name: "prose preamble",
			in:   `Here is the JSON: {"a":1}`,
			want: `{"a":1}`,
		},
		{
			name: "preamble + fence + trailing prose",
			in:   "Sure! Here's your problem:\n\n```json\n{\"a\":1,\"b\":2}\n```\n\nLet me know if you need adjustments.",
			want: `{"a":1,"b":2}`,
		},
		{
			name: "nested objects",
			in:   `{"a": {"b": {"c": 1}}}`,
			want: `{"a": {"b": {"c": 1}}}`,
		},
		{
			name: "string with brace inside",
			in:   `{"a": "value with } brace"}`,
			want: `{"a": "value with } brace"}`,
		},
		{
			name: "string with escaped quote",
			in:   `{"a": "she said \"hi\""}`,
			want: `{"a": "she said \"hi\""}`,
		},
		{
			name: "array of nested objects",
			in:   `Here are the choices: [{"x":1},{"y":2}]`,
			want: `[{"x":1},{"y":2}]`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractJSON(tc.in)
			if !strings.Contains(got, tc.want) {
				t.Errorf("ExtractJSON(%q):\n  got:  %q\n  want substring: %q", tc.in, got, tc.want)
			}
			// Verify the result actually parses as JSON
			var anyVal any
			if err := json.Unmarshal([]byte(got), &anyVal); err != nil {
				t.Errorf("result %q does not parse as JSON: %v", got, err)
			}
		})
	}
}

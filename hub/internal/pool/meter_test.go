package pool

import (
	"io"
	"strings"
	"testing"
)

// The meter finds the counts in every shape the router passes through,
// and passes every byte on unchanged.
func TestMeter(t *testing.T) {
	for _, c := range []struct {
		name, body string
		in, out    int64
	}{
		{"ollama stream", "{\"done\":false}\n{\"done\":true,\"prompt_eval_count\":12,\"eval_count\":34}\n", 12, 34},
		{"ollama whole", `{"done":true,"prompt_eval_count":5,"eval_count":6}`, 5, 6},
		{"openai whole", `{"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":8}}`, 7, 8},
		{"openai sse", "data: {\"choices\":[]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n", 1, 2},
		{"none", "oops", 0, 0},
	} {
		m := &meter{r: strings.NewReader(c.body)}
		got, _ := io.ReadAll(m)
		if string(got) != c.body || m.input != c.in || m.output != c.out {
			t.Errorf("%s: got %d/%d, want %d/%d", c.name, m.input, m.output, c.in, c.out)
		}
	}
}

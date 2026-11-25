package builder

import (
	"slices"

	"github.com/kart-io/goagent/llm"
)

// UserInput is a structured user input carrying text and image references.
// Pass it (or a pointer to it) as the input of ExecuteStream; any other input
// type still goes through the configured InputSerializer. The images are
// recorded in the history and the session log as given, normally as
// references with an empty URL; the resolver configured with
// WithImageResolver fills URLs only in outgoing model requests.
type UserInput struct {
	Text   string
	Images []llm.ImagePart
}

// splitUserInput 识别结构化输入；ok 为 false 时调用方走原有序列化。
func splitUserInput(v interface{}) (text string, images []llm.ImagePart, ok bool) {
	switch u := v.(type) {
	case UserInput:
		return u.Text, slices.Clone(u.Images), true
	case *UserInput:
		if u != nil {
			return u.Text, slices.Clone(u.Images), true
		}
	}
	return "", nil, false
}

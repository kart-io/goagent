package skills

import (
	"cmp"
	"slices"
	"strings"
)

// Prompt renders the skills the model may invoke as one "name: description"
// line per skill, sorted by name, for embedding in a system prompt. Skills
// with DisableModelInvocation set are omitted. It returns the empty string
// when nothing is listed. Descriptions are collapsed onto a single line.
func (c *Catalog) Prompt() string {
	list := slices.DeleteFunc(c.List(), func(m Metadata) bool { return m.DisableModelInvocation })
	slices.SortFunc(list, func(a, b Metadata) int { return cmp.Compare(a.Name, b.Name) })
	lines := make([]string, len(list))
	for i, m := range list {
		lines[i] = m.Name + ": " + strings.Join(strings.Fields(m.Description), " ")
	}
	return strings.Join(lines, "\n")
}

package interfaces

// RetrievalSource identifies reference material actually sent to the model; it
// does not mean the citation has passed fact-checking.
type RetrievalSource struct {
	Reference int     `json:"reference"`
	ID        string  `json:"id"`
	Source    string  `json:"source,omitempty"`
	Score     float64 `json:"score"`
}

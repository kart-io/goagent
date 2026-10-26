package interfaces

// RetrievalSource 标识实际送入模型的参考资料，不代表引用已经通过事实核验。
type RetrievalSource struct {
	Reference int     `json:"reference"`
	ID        string  `json:"id"`
	Source    string  `json:"source,omitempty"`
	Score     float64 `json:"score"`
}

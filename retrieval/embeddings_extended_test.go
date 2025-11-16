package retrieval

import (
	"context"
	"math"
	"testing"
)

// TestEuclideanDistance tests euclidean distance calculation
func TestEuclideanDistance(t *testing.T) {
	tests := []struct {
		name     string
		vec1     []float32
		vec2     []float32
		expected float32
		wantErr  bool
	}{
		{
			name:     "Identical vectors zero distance",
			vec1:     []float32{1.0, 2.0, 3.0},
			vec2:     []float32{1.0, 2.0, 3.0},
			expected: 0.0,
			wantErr:  false,
		},
		{
			name:     "Simple distance calculation",
			vec1:     []float32{0, 0},
			vec2:     []float32{3, 4},
			expected: 5.0,
			wantErr:  false,
		},
		{
			name:     "Negative values",
			vec1:     []float32{-1.0, -2.0},
			vec2:     []float32{1.0, 2.0},
			expected: float32(math.Sqrt(20)),
			wantErr:  false,
		},
		{
			name:     "Length mismatch error",
			vec1:     []float32{1.0, 2.0},
			vec2:     []float32{1.0, 2.0, 3.0},
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "Empty vectors",
			vec1:     []float32{},
			vec2:     []float32{},
			expected: 0,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dist, err := euclideanDistance(tt.vec1, tt.vec2)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}

				// Allow small floating point error
				if math.Abs(float64(dist-tt.expected)) > 0.01 {
					t.Errorf("Expected distance %.2f, got %.2f", tt.expected, dist)
				}
			}
		})
	}
}

// TestDotProduct tests dot product calculation
func TestDotProduct(t *testing.T) {
	tests := []struct {
		name     string
		vec1     []float32
		vec2     []float32
		expected float32
		wantErr  bool
	}{
		{
			name:     "Basic dot product",
			vec1:     []float32{1.0, 2.0, 3.0},
			vec2:     []float32{4.0, 5.0, 6.0},
			expected: 32.0, // 1*4 + 2*5 + 3*6 = 4 + 10 + 18 = 32
			wantErr:  false,
		},
		{
			name:     "Orthogonal vectors",
			vec1:     []float32{1.0, 0, 0},
			vec2:     []float32{0, 1.0, 0},
			expected: 0.0,
			wantErr:  false,
		},
		{
			name:     "Negative values",
			vec1:     []float32{-1.0, -2.0},
			vec2:     []float32{3.0, 4.0},
			expected: -11.0, // (-1)*3 + (-2)*4 = -3 + -8 = -11
			wantErr:  false,
		},
		{
			name:     "Length mismatch error",
			vec1:     []float32{1.0, 2.0},
			vec2:     []float32{1.0},
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "Empty vectors",
			vec1:     []float32{},
			vec2:     []float32{},
			expected: 0,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := dotProduct(tt.vec1, tt.vec2)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}

				if math.Abs(float64(result-tt.expected)) > 0.01 {
					t.Errorf("Expected product %.2f, got %.2f", tt.expected, result)
				}
			}
		})
	}
}

// TestCosineSimilarityEdgeCases tests cosine similarity edge cases
func TestCosineSimilarityEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		vec1     []float32
		vec2     []float32
		expected float32
		wantErr  bool
	}{
		{
			name:     "Zero vector",
			vec1:     []float32{0, 0, 0},
			vec2:     []float32{1.0, 2.0, 3.0},
			expected: 0.0,
			wantErr:  false,
		},
		{
			name:     "Both zero vectors",
			vec1:     []float32{0, 0},
			vec2:     []float32{0, 0},
			expected: 0.0,
			wantErr:  false,
		},
		{
			name:     "Opposite vectors",
			vec1:     []float32{1.0, 0, 0},
			vec2:     []float32{-1.0, 0, 0},
			expected: -1.0,
			wantErr:  false,
		},
		{
			name:     "Single dimension",
			vec1:     []float32{5.0},
			vec2:     []float32{10.0},
			expected: 1.0,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sim, err := cosineSimilarity(tt.vec1, tt.vec2)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}

				if math.Abs(float64(sim-tt.expected)) > 0.01 {
					t.Errorf("Expected similarity %.2f, got %.2f", tt.expected, sim)
				}
			}
		})
	}
}

// TestNormalizeVector tests vector normalization
func TestNormalizeVector(t *testing.T) {
	tests := []struct {
		name      string
		vec       []float32
		checkNorm bool // Should check if result is unit length
	}{
		{
			name:      "Zero vector",
			vec:       []float32{0, 0, 0},
			checkNorm: false,
		},
		{
			name:      "Unit vector",
			vec:       []float32{1.0, 0, 0},
			checkNorm: true,
		},
		{
			name:      "Random vector",
			vec:       []float32{3.0, 4.0},
			checkNorm: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized := normalizeVector(tt.vec)

			if len(normalized) != len(tt.vec) {
				t.Errorf("Expected length %d, got %d", len(tt.vec), len(normalized))
			}

			if tt.checkNorm && len(tt.vec) > 0 {
				// Check if result is unit length (approximately)
				var norm float32
				for _, v := range normalized {
					norm += v * v
				}
				norm = float32(math.Sqrt(float64(norm)))

				if math.Abs(float64(norm-1.0)) > 0.01 {
					t.Errorf("Expected unit norm, got %.2f", norm)
				}
			}
		})
	}
}

// TestSimpleEmbedderEmptyTexts tests embedding empty text list
func TestSimpleEmbedderEmptyTexts(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(50)

	vectors, err := embedder.Embed(ctx, []string{})
	if err != nil {
		t.Fatalf("Embedding empty list failed: %v", err)
	}

	if len(vectors) != 0 {
		t.Errorf("Expected 0 vectors for empty input, got %d", len(vectors))
	}
}

// TestSimpleEmbedderConsistency tests embedding consistency
func TestSimpleEmbedderConsistency(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(100)

	text := "Machine learning is powerful"

	vec1, err := embedder.EmbedQuery(ctx, text)
	if err != nil {
		t.Fatalf("First embedding failed: %v", err)
	}

	vec2, err := embedder.EmbedQuery(ctx, text)
	if err != nil {
		t.Fatalf("Second embedding failed: %v", err)
	}

	// Same text should produce same embedding
	for i, v := range vec1 {
		if v != vec2[i] {
			t.Errorf("Embeddings not consistent at index %d: %.2f != %.2f", i, v, vec2[i])
		}
	}
}

// TestSimpleEmbedderVariability tests that different texts produce different embeddings
func TestSimpleEmbedderVariability(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(100)

	texts := []string{
		"Machine learning models",
		"Deep learning neural networks",
		"Natural language processing",
	}

	vectors, err := embedder.Embed(ctx, texts)
	if err != nil {
		t.Fatalf("Embedding failed: %v", err)
	}

	// Check that different texts produce somewhat different embeddings
	if len(vectors) < 2 {
		t.Fatal("Expected at least 2 vectors")
	}

	// Calculate similarity between first two vectors
	sim, err := cosineSimilarity(vectors[0], vectors[1])
	if err != nil {
		t.Fatalf("Similarity calculation failed: %v", err)
	}

	// Different texts should not have perfect similarity
	if sim > 0.99 {
		t.Errorf("Different texts produced too similar embeddings: %.2f", sim)
	}
}

// TestSimpleEmbedderWithDifferentDimensions tests different embedding dimensions
func TestSimpleEmbedderWithDifferentDimensions(t *testing.T) {
	ctx := context.Background()

	dimensions := []int{10, 50, 100, 256, 768}

	for _, dim := range dimensions {
		t.Run("Dimension "+string(rune(dim)), func(t *testing.T) {
			embedder := NewSimpleEmbedder(dim)

			if embedder.Dimensions() != dim {
				t.Errorf("Expected dimensions %d, got %d", dim, embedder.Dimensions())
			}

			vectors, err := embedder.Embed(ctx, []string{"test text"})
			if err != nil {
				t.Fatalf("Embedding failed: %v", err)
			}

			if len(vectors[0]) != dim {
				t.Errorf("Expected vector dimension %d, got %d", dim, len(vectors[0]))
			}
		})
	}
}

// TestSimpleEmbedderZeroDimension tests zero dimension handling
func TestSimpleEmbedderZeroDimension(t *testing.T) {
	embedder := NewSimpleEmbedder(0)

	// Should default to 100
	if embedder.Dimensions() != 100 {
		t.Errorf("Expected default dimension 100, got %d", embedder.Dimensions())
	}
}

// TestSimpleEmbedderLargeText tests embedding very long text
func TestSimpleEmbedderLargeText(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(100)

	// Create large text
	largeText := ""
	for i := 0; i < 1000; i++ {
		largeText += "word "
	}

	vector, err := embedder.EmbedQuery(ctx, largeText)
	if err != nil {
		t.Fatalf("Embedding large text failed: %v", err)
	}

	if len(vector) != 100 {
		t.Errorf("Expected vector dimension 100, got %d", len(vector))
	}
}

// TestSimpleEmbedderSpecialCharacters tests embedding text with special characters
func TestSimpleEmbedderSpecialCharacters(t *testing.T) {
	ctx := context.Background()
	embedder := NewSimpleEmbedder(100)

	texts := []string{
		"Hello @#$% World!",
		"Machine-Learning AI/ML",
		"Numbers 12345 67890",
		"日本語テキスト",
	}

	vectors, err := embedder.Embed(ctx, texts)
	if err != nil {
		t.Fatalf("Embedding special characters failed: %v", err)
	}

	if len(vectors) != len(texts) {
		t.Errorf("Expected %d vectors, got %d", len(texts), len(vectors))
	}

	for i, vec := range vectors {
		if len(vec) != 100 {
			t.Errorf("Vector %d: expected dimension 100, got %d", i, len(vec))
		}
	}
}

// TestBaseEmbedderDimensions tests base embedder dimensions
func TestBaseEmbedderDimensions(t *testing.T) {
	tests := []int{10, 50, 100, 256, 512}

	for _, dim := range tests {
		t.Run("Dimensions "+string(rune(dim)), func(t *testing.T) {
			embedder := NewBaseEmbedder(dim)

			if embedder.Dimensions() != dim {
				t.Errorf("Expected %d, got %d", dim, embedder.Dimensions())
			}
		})
	}
}

package cache

import (
	"math"
)

// CosineSimilarity calculates the cosine similarity between two vectors
// Returns a value between -1 and 1, where 1 means identical direction
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float64

	for i := 0; i < len(a); i++ {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// EuclideanDistance calculates the Euclidean distance between two vectors
func EuclideanDistance(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return math.MaxFloat64
	}

	var sum float64
	for i := 0; i < len(a); i++ {
		diff := float64(a[i]) - float64(b[i])
		sum += diff * diff
	}

	return math.Sqrt(sum)
}

// DotProduct calculates the dot product of two vectors
func DotProduct(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var result float64
	for i := 0; i < len(a); i++ {
		result += float64(a[i]) * float64(b[i])
	}

	return result
}

// Normalize normalizes a vector to unit length
func Normalize(v []float32) []float32 {
	if len(v) == 0 {
		return v
	}

	var norm float64
	for _, val := range v {
		norm += float64(val) * float64(val)
	}

	if norm == 0 {
		return v
	}

	norm = math.Sqrt(norm)
	result := make([]float32, len(v))
	for i, val := range v {
		result[i] = float32(float64(val) / norm)
	}

	return result
}

// FindMostSimilar finds the most similar entry from a list
// Returns the entry, similarity score, and index
func FindMostSimilar(target []float32, entries []*CacheEntry) (*CacheEntry, float64, int) {
	if len(entries) == 0 || len(target) == 0 {
		return nil, 0, -1
	}

	var bestEntry *CacheEntry
	bestSimilarity := float64(-1)
	bestIndex := -1

	for i, entry := range entries {
		if entry == nil || len(entry.Embedding) == 0 {
			continue
		}

		similarity := CosineSimilarity(target, entry.Embedding)
		if similarity > bestSimilarity {
			bestSimilarity = similarity
			bestEntry = entry
			bestIndex = i
		}
	}

	return bestEntry, bestSimilarity, bestIndex
}

// FindAboveThreshold finds all entries with similarity above threshold
func FindAboveThreshold(target []float32, entries []*CacheEntry, threshold float64) []*CacheEntry {
	if len(entries) == 0 || len(target) == 0 {
		return nil
	}

	var results []*CacheEntry
	for _, entry := range entries {
		if entry == nil || len(entry.Embedding) == 0 {
			continue
		}

		similarity := CosineSimilarity(target, entry.Embedding)
		if similarity >= threshold {
			results = append(results, entry)
		}
	}

	return results
}

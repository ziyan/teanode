package faces

import "math"

// CosineSimilarity is how nearly two vectors point the same way: 1 for the
// same direction, 0 for unrelated, -1 for opposite. SFace's published
// threshold for the same person is 0.363. Vectors of different lengths, and
// the zero vector, are unrelated to everything.
func CosineSimilarity(first, second []float32) float64 {
	if len(first) != len(second) || len(first) == 0 {
		return 0
	}
	var dot, firstSquares, secondSquares float64
	for index := range first {
		dot += float64(first[index]) * float64(second[index])
		firstSquares += float64(first[index]) * float64(first[index])
		secondSquares += float64(second[index]) * float64(second[index])
	}
	if firstSquares == 0 || secondSquares == 0 {
		return 0
	}
	return dot / math.Sqrt(firstSquares*secondSquares)
}

// Normalize returns vector scaled to unit length, as a new slice. The zero
// vector has no direction and comes back as zeros.
func Normalize(vector []float32) []float32 {
	normalized := make([]float32, len(vector))
	var squares float64
	for _, number := range vector {
		squares += float64(number) * float64(number)
	}
	if squares == 0 {
		return normalized
	}
	length := math.Sqrt(squares)
	for index, number := range vector {
		normalized[index] = float32(float64(number) / length)
	}
	return normalized
}

// MeanVector is the average direction of the vectors, at unit length: the
// vector that stands for a group of faces. Vectors of different lengths have
// no mean, and nor does no vector.
func MeanVector(vectors [][]float32) []float32 {
	if len(vectors) == 0 {
		return nil
	}
	sums := make([]float64, len(vectors[0]))
	for _, vector := range vectors {
		if len(vector) != len(sums) {
			return nil
		}
		for index, number := range vector {
			sums[index] += float64(number)
		}
	}
	mean := make([]float32, len(sums))
	for index, sum := range sums {
		mean[index] = float32(sum / float64(len(vectors)))
	}
	return Normalize(mean)
}

package faces

import (
	"math"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	for name, test := range map[string]struct {
		first, second []float32
		want          float64
	}{
		"same direction":      {[]float32{1, 2, 3}, []float32{2, 4, 6}, 1},
		"opposite":            {[]float32{1, 0}, []float32{-3, 0}, -1},
		"at right angles":     {[]float32{1, 0}, []float32{0, 5}, 0},
		"at forty five":       {[]float32{1, 0}, []float32{1, 1}, math.Sqrt2 / 2},
		"different lengths":   {[]float32{1, 2}, []float32{1, 2, 3}, 0},
		"zero vector":         {[]float32{0, 0}, []float32{1, 2}, 0},
		"nothing at all":      {nil, nil, 0},
		"large and small mix": {[]float32{1e10, 1}, []float32{1e10, 1}, 1},
	} {
		if got := CosineSimilarity(test.first, test.second); math.Abs(got-test.want) > 1e-9 {
			t.Errorf("%s: CosineSimilarity = %v, want %v", name, got, test.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	original := []float32{3, 4}
	normalized := Normalize(original)
	if math.Abs(float64(normalized[0])-0.6) > 1e-6 || math.Abs(float64(normalized[1])-0.8) > 1e-6 {
		t.Errorf("Normalize = %v", normalized)
	}
	if original[0] != 3 || original[1] != 4 {
		t.Errorf("the input was changed: %v", original)
	}
	if zeros := Normalize([]float32{0, 0, 0}); zeros[0] != 0 || len(zeros) != 3 {
		t.Errorf("zero vector = %v", zeros)
	}
	if len(Normalize(nil)) != 0 {
		t.Error("nothing should normalize to nothing")
	}
}

func TestMeanVector(t *testing.T) {
	mean := MeanVector([][]float32{{1, 0}, {0, 1}})
	if len(mean) != 2 || math.Abs(float64(mean[0])-math.Sqrt2/2) > 1e-6 || math.Abs(float64(mean[1])-math.Sqrt2/2) > 1e-6 {
		t.Errorf("mean = %v", mean)
	}
	// The mean of one vector is its direction.
	if single := MeanVector([][]float32{{0, 3}}); single[0] != 0 || math.Abs(float64(single[1])-1) > 1e-6 {
		t.Errorf("single = %v", single)
	}
	// Vectors that cancel out have no direction.
	if cancelled := MeanVector([][]float32{{1, 0}, {-1, 0}}); cancelled[0] != 0 || cancelled[1] != 0 {
		t.Errorf("cancelled = %v", cancelled)
	}
	if MeanVector(nil) != nil {
		t.Error("no vectors have no mean")
	}
	if MeanVector([][]float32{{1, 2}, {1}}) != nil {
		t.Error("vectors of different lengths have no mean")
	}
}

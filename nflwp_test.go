package nflwp

import (
	"math"
	"testing"
)

func TestCDF(t *testing.T) {
	xs := []float64{0, -2, -1, 1, 2}
	expectedResults := []float64{0.5, 0.0228, 0.1587, 0.8413, 0.9772}
	for i := 0; i < len(xs); i++ {
		result := cdf(xs[i], 0, 1)
		if result-expectedResults[i] > 0.0005 {
			t.Errorf("We got an unexpected result: %v instead of %v", result, expectedResults[i])
		}
	}
}

func TestNewSpread(t *testing.T) {
	spreads := []float64{-5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, -10, 10}
	for i := 0; i < len(spreads); i++ {
		result := NewSpread(0.5, spreads[i], STDDEV)
		if result > 0.0005 {
			t.Errorf("We got an unexpected result: %v instead of %v", result, 0.0)
		}
	}
}

func TestGuessSpread(t *testing.T) {
	probs := []float64{0.5, 0.53, 0.47, 0.588, 0.4120, 0.6990, 0.3010}
	expectedResults := []float64{0.0, -1, 1, -3, 3, -7, 7}
	for i := 0; i < len(probs); i++ {
		result := GuessSpread(probs[i], STDDEV)
		if math.Abs(result-expectedResults[i]) > 0.01 {
			t.Errorf("We got an unexpected result: %v instead of %v", result, expectedResults[i])
		}
	}
}

func TestFindAdjustedStartingProbability(t *testing.T) {
	// A few random examples that we calculated on pro-football-reference.com
	// And one to test the error
	// It seems that pro-football-reference.com handles the 4th and OT differently than the other quarters.
	spreads := []float64{-7, 3, -5, 0, 10, 7.0}
	infos := []string{"\"Q1 5:00 GNB 0-CHI 0 32.20%\"", "\"Q2 12:00 GNB 0-CHI 0 32.20%\"",
		"\"Q3 10:00 GNB 0-CHI 0 32.20%\"", "\"Q4 2:00 GNB 0-CHI 0 32.20%\"",
		"\"Q3 2:00 GNB 0-CHI 0 32.20%\"", "   "}
	expectedResults := []float64{0.6830, 0.4260, 0.5950, 0.5, 0.3460, 9.0}
	for i := 0; i < len(spreads); i++ {
		result := FindAdjustedStartingProbability(spreads[i], infos[i], 9.0)
		if math.Abs(result-expectedResults[i]) > 0.0005 {
			t.Errorf("We got an unexpected result: %v instead of %v", result, expectedResults[i])
		}
	}
}

package automix

import "math"

// CamelotKey is standard wheel notation: 1..12 with A=minor, B=major.
type CamelotKey struct {
	Number int
	Letter string
}

func (k CamelotKey) Code() string {
	return itoa(k.Number) + k.Letter
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	digits := make([]byte, 0, 4)
	for v > 0 {
		digits = append(digits, byte('0'+v%10))
		v /= 10
	}
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}

var minorOrder = [12]int{4, 11, 6, 1, 8, 3, 10, 5, 0, 7, 2, 9}
var majorOrder = [12]int{7, 2, 9, 4, 11, 6, 1, 8, 3, 10, 5, 0}

// CamelotFor maps chroma + mode to the wheel.
func CamelotFor(r *Result) *CamelotKey {
	if r == nil || r.KeyPitchClass == nil {
		return nil
	}
	order := majorOrder
	letter := "B"
	if r.KeyIsMinor != nil && *r.KeyIsMinor {
		order, letter = minorOrder, "A"
	}
	pc := clampInt(*r.KeyPitchClass, 0, 11)
	for i, value := range order {
		if value == pc {
			return &CamelotKey{Number: i + 1, Letter: letter}
		}
	}
	return nil
}

// HarmonicScore rates a transition: same key 1, relative 0.85, neighbour 0.7.
func HarmonicScore(from, to *CamelotKey) float64 {
	if from == nil || to == nil {
		return 0
	}
	if *from == *to {
		return 1
	}
	if from.Number == to.Number {
		return 0.85
	}
	delta := from.Number - to.Number
	if delta < 0 {
		delta = -delta
	}
	if from.Letter == to.Letter && (delta == 1 || delta == 11) {
		return 0.7
	}
	return 0
}

// PickHarmonicNext chooses the most compatible candidate, preferring the
// natural next track on ties so the queue is perturbed as little as possible.
func PickHarmonicNext(nextIndex int, candidates []int, from *CamelotKey, keys map[int]*CamelotKey) int {
	best, bestScore := nextIndex, math.Inf(-1)
	for _, index := range candidates {
		score := HarmonicScore(from, keys[index])
		delta := index - nextIndex
		if delta < 0 {
			delta = -delta
		}
		score -= float64(delta) / 1000
		if score > bestScore {
			best, bestScore = index, score
		}
	}
	return best
}

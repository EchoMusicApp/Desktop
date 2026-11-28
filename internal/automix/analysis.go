// Package automix ports the advanced automix algorithms from the Android and
// Flutter clients: beat-grid analysis, Camelot harmonic compatibility,
// 16-beat blends and beat-aligned crossfade start offsets. All DSP is pure Go
// so it can be unit-tested with synthetic PCM.
package automix

import (
	"encoding/json"
	"errors"
	"math"
)

// Result is the analysis of one track.
type Result struct {
	BPM               float64 `json:"bpm"`
	FirstBeatOffsetMs float64 `json:"firstBeatOffsetMs"`
	Confidence        float64 `json:"confidence"`
	MixInMs           *int    `json:"mixInMs,omitempty"`
	MixOutMs          *int    `json:"mixOutMs,omitempty"`
	KeyPitchClass     *int    `json:"keyPitchClass,omitempty"`
	KeyIsMinor        *bool   `json:"keyIsMinor,omitempty"`
}

func (r Result) BeatPeriodMs() float64 {
	if r.BPM <= 0 {
		return 500
	}
	return 60000 / r.BPM
}

func (r Result) KeyLabel() string {
	if r.KeyPitchClass == nil {
		return ""
	}
	names := [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}
	label := names[clampInt(*r.KeyPitchClass, 0, 11)]
	if r.KeyIsMinor != nil && *r.KeyIsMinor {
		label += "m"
	}
	return label
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// FFT is an in-place iterative radix-2 FFT.
func FFT(re, im []float64) error {
	n := len(re)
	if n < 2 || n&(n-1) != 0 || len(im) != n {
		return errors.New("automix: FFT size must be a power of two")
	}
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for j&bit != 0 {
			j ^= bit
			bit >>= 1
		}
		j ^= bit
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	for length := 2; length <= n; length <<= 1 {
		angle := -2 * math.Pi / float64(length)
		wLenRe, wLenIm := math.Cos(angle), math.Sin(angle)
		for i := 0; i < n; i += length {
			wRe, wIm := 1.0, 0.0
			for k := 0; k < length/2; k++ {
				uRe, uIm := re[i+k], im[i+k]
				vRe := re[i+k+length/2]*wRe - im[i+k+length/2]*wIm
				vIm := re[i+k+length/2]*wIm + im[i+k+length/2]*wRe
				re[i+k], im[i+k] = uRe+vRe, uIm+vIm
				re[i+k+length/2], im[i+k+length/2] = uRe-vRe, uIm-vIm
				wRe, wIm = wRe*wLenRe-wIm*wLenIm, wRe*wLenIm+wIm*wLenRe
			}
		}
	}
	return nil
}

// SpectralFlux returns the positive spectral difference envelope.
func SpectralFlux(mono []float64, fftSize, hop int) []float64 {
	if fftSize <= 0 || hop <= 0 {
		return nil
	}
	if len(mono) < fftSize*4 {
		return nil
	}
	window := make([]float64, fftSize)
	for i := range window {
		window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(fftSize-1))
	}
	flux := make([]float64, 0, len(mono)/hop)
	previous := make([]float64, fftSize/2)
	for start := 0; start+fftSize <= len(mono); start += hop {
		re, im := make([]float64, fftSize), make([]float64, fftSize)
		for i := 0; i < fftSize; i++ {
			re[i] = mono[start+i] * window[i]
		}
		if FFT(re, im) != nil {
			return nil
		}
		sum := 0.0
		for i := 0; i < fftSize/2; i++ {
			magnitude := math.Hypot(re[i], im[i])
			if diff := magnitude - previous[i]; diff > 0 {
				sum += diff
			}
			previous[i] = magnitude
		}
		flux = append(flux, sum/float64(fftSize/2))
	}
	return flux
}

// EstimateTempo autocorrelates the onset envelope between 60 and 180 BPM.
func EstimateTempo(flux []float64, frameRate float64) (bpm, confidence float64) {
	if len(flux) < 32 {
		return 120, 0
	}
	mean := 0.0
	for _, v := range flux {
		mean += v
	}
	mean /= float64(len(flux))
	centered := make([]float64, len(flux))
	for i, v := range flux {
		centered[i] = v - mean
	}
	bpm = 120
	bestScore := math.Inf(-1)
	for candidate := 60.0; candidate <= 180.0; candidate += 0.5 {
		lag := int(frameRate * 60.0 / candidate)
		if lag < 2 || lag >= len(centered)/2 {
			continue
		}
		dot, norm := 0.0, 0.0
		for i := 0; i+lag < len(centered); i++ {
			dot += centered[i] * centered[i+lag]
			norm += centered[i] * centered[i]
		}
		score := 0.0
		if norm > 0 {
			score = dot / norm
		}
		if score > bestScore {
			bestScore, bpm = score, candidate
		}
	}
	for bpm < 70 {
		bpm *= 2
	}
	for bpm > 140 {
		bpm /= 2
	}
	return bpm, clampFloat(bestScore, 0, 1)
}

// EstimateBeatPhase finds where onsets line up inside a beat period.
func EstimateBeatPhase(flux []float64, frameRate, bpm float64) float64 {
	if len(flux) == 0 || frameRate <= 0 || bpm <= 0 {
		return 0
	}
	period := clampInt(int(frameRate*60/bpm), 2, len(flux)/2)
	if period < 2 {
		return 0
	}
	bestPhase, bestSum := 0, math.Inf(-1)
	for phase := 0; phase < period; phase++ {
		sum := 0.0
		for i := phase; i < len(flux); i += period {
			sum += flux[i]
		}
		if sum > bestSum {
			bestSum, bestPhase = sum, phase
		}
	}
	return float64(bestPhase) / frameRate * 1000
}

func blockEnergies(mono []float64, sampleRate, blockMs int) []float64 {
	block := sampleRate * blockMs / 1000
	if block <= 0 || len(mono) < block {
		return nil
	}
	energies := make([]float64, 0, len(mono)/block)
	for start := 0; start+block <= len(mono); start += block {
		sum := 0.0
		for i := start; i < start+block; i++ {
			sum += mono[i] * mono[i]
		}
		energies = append(energies, math.Sqrt(sum/float64(block)))
	}
	return energies
}

// EnergyMixIn finds the first energetic block after the intro.
func EnergyMixIn(mono []float64, sampleRate, blockMs, maxSkipMs int) *int {
	energies := blockEnergies(mono, sampleRate, blockMs)
	if len(energies) < 3 {
		return nil
	}
	peak := 0.0
	for _, e := range energies {
		peak = math.Max(peak, e)
	}
	if peak <= 0 {
		return nil
	}
	for i, e := range energies {
		if e >= peak*0.35 {
			v := min(i*blockMs, maxSkipMs)
			return &v
		}
	}
	return nil
}

// EnergyMixOut finds the last energetic block of the tail window.
func EnergyMixOut(tail []float64, sampleRate, durationMs, blockMs, maxCutMs int) *int {
	energies := blockEnergies(tail, sampleRate, blockMs)
	if len(energies) < 2 || durationMs <= 0 {
		return nil
	}
	peak, last := 0.0, -1
	for _, e := range energies {
		peak = math.Max(peak, e)
	}
	if peak <= 0 {
		return nil
	}
	for i, e := range energies {
		if e >= peak*0.35 {
			last = i
		}
	}
	if last < 0 {
		return nil
	}
	windowMs := int(float64(len(tail)) / float64(sampleRate) * 1000)
	absolute := durationMs - windowMs + last*blockMs + blockMs/2
	if absolute < durationMs-maxCutMs {
		return nil
	}
	v := clampInt(absolute, 0, durationMs)
	return &v
}

var majorProfile = [12]float64{6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88}
var minorProfile = [12]float64{6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17}

// EstimateKey correlates chroma with Krumhansl-Schmuckler profiles.
func EstimateKey(mono []float64, sampleRate, fftSize int) (pitchClass int, minor bool, ok bool) {
	if sampleRate <= 0 || fftSize <= 0 || len(mono) < fftSize*4 {
		return 0, false, false
	}
	var chroma [12]float64
	for start := 0; start+fftSize <= len(mono); start += fftSize / 2 {
		re, im := make([]float64, fftSize), make([]float64, fftSize)
		for i := 0; i < fftSize; i++ {
			re[i] = mono[start+i]
		}
		if FFT(re, im) != nil {
			return 0, false, false
		}
		for i := 1; i < fftSize/2; i++ {
			freq := float64(i) * float64(sampleRate) / float64(fftSize)
			if freq < 80 || freq > 2000 {
				continue
			}
			magnitude := math.Hypot(re[i], im[i])
			midi := 69 + 12*math.Log(freq/440)/math.Ln2
			pc := ((int(math.Round(midi)) % 12) + 12) % 12
			chroma[pc] += magnitude * magnitude
		}
	}
	total := 0.0
	for _, v := range chroma {
		total += v
	}
	if total <= 0 {
		return 0, false, false
	}
	bestScore := math.Inf(-1)
	for tonic := 0; tonic < 12; tonic++ {
		for _, isMinor := range []bool{false, true} {
			profile := majorProfile
			if isMinor {
				profile = minorProfile
			}
			var rotated [12]float64
			for pc := 0; pc < 12; pc++ {
				rotated[pc] = profile[(pc-tonic+12)%12]
			}
			score := correlation(chroma[:], rotated[:])
			if score > bestScore {
				bestScore, pitchClass, minor = score, tonic, isMinor
			}
		}
	}
	return pitchClass, minor, true
}

func correlation(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	am, bm := 0.0, 0.0
	for i := range a {
		am += a[i]
		bm += b[i]
	}
	am /= float64(len(a))
	bm /= float64(len(b))
	num, da, db := 0.0, 0.0, 0.0
	for i := range a {
		x, y := a[i]-am, b[i]-bm
		num += x * y
		da += x * x
		db += y * y
	}
	if da <= 0 || db <= 0 {
		return 0
	}
	return num / math.Sqrt(da*db)
}

// Analyze runs the full beat/key analysis on 22.05 kHz mono PCM.
func Analyze(mono []float32, sampleRate int, tail []float32, durationMs int) *Result {
	if sampleRate <= 0 || len(mono) < 22050 {
		return nil
	}
	samples := make([]float64, len(mono))
	for i, v := range mono {
		samples[i] = float64(v)
	}
	flux := SpectralFlux(samples, 1024, 512)
	if len(flux) < 32 {
		return nil
	}
	bpm, confidence := EstimateTempo(flux, float64(sampleRate)/512)
	if confidence <= 0 {
		return nil
	}
	phase := EstimateBeatPhase(flux, float64(sampleRate)/512, bpm)
	result := &Result{BPM: bpm, FirstBeatOffsetMs: phase, Confidence: confidence}
	result.MixInMs = EnergyMixIn(samples, sampleRate, 500, 20000)
	if len(tail) > 0 {
		tailSamples := make([]float64, len(tail))
		for i, v := range tail {
			tailSamples[i] = float64(v)
		}
		result.MixOutMs = EnergyMixOut(tailSamples, sampleRate, durationMs, 500, 45000)
	}
	if pc, isMinor, ok := EstimateKey(samples, sampleRate, 4096); ok {
		result.KeyPitchClass = &pc
		result.KeyIsMinor = &isMinor
	}
	return result
}

// DecodeJSON loads a persisted analysis.
func DecodeJSON(data []byte) (*Result, error) {
	var r Result
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	if r.BPM <= 0 {
		return nil, errors.New("automix: invalid analysis")
	}
	return &r, nil
}

// EncodeJSON persists an analysis.
func EncodeJSON(r *Result) ([]byte, error) { return json.Marshal(r) }

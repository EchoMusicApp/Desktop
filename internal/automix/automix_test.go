package automix

import (
	"math"
	"testing"
	"time"
)

func TestFFTAndTempo(t *testing.T) {
	re, im := make([]float64, 8), make([]float64, 8)
	re[0] = 1
	if err := FFT(re, im); err != nil {
		t.Fatal(err)
	}
	for i := range re {
		if math.Abs(re[i]-1) > 1e-9 || math.Abs(im[i]) > 1e-9 {
			t.Fatalf("FFT of a constant = %v %v", re, im)
		}
	}
	// Synthetic 120 BPM clicks, 50 frames/s.
	flux := make([]float64, 600)
	for i := range flux {
		if i%25 == 0 {
			flux[i] = 1
		}
	}
	bpm, confidence := EstimateTempo(flux, 50)
	if math.Abs(bpm-120) > 1 || confidence <= 0 {
		t.Fatalf("tempo = %.1f confidence %.2f", bpm, confidence)
	}
	if phase := EstimateBeatPhase(flux, 50, bpm); phase > 25 {
		t.Fatalf("phase = %.1f ms", phase)
	}
}

func TestBlendAndPhaseAlignment(t *testing.T) {
	result := &Result{BPM: 120, FirstBeatOffsetMs: 100}
	if got := BlendDurationMs(result, 16, 4000, 15000); got != 8000 {
		t.Fatalf("blend = %d ms", got)
	}
	if got := TriggerMs(180000, 8000, result, nil); got < 8000 || got > 180000 {
		t.Fatalf("trigger = %d ms", got)
	}
	incoming := &Result{BPM: 120, FirstBeatOffsetMs: 500, MixInMs: intPtr(2000)}
	start := AlignedStartMs(incoming, 8000, 30000)
	if start < 2000 || start > 30000 {
		t.Fatalf("aligned start = %d ms", start)
	}
	if got := PreArmLeadMs([]float64{900, 1100, 1000}, 3000); got < 800 || got > 2000 {
		t.Fatalf("pre-arm lead = %d ms", got)
	}
}

func TestCamelotHarmony(t *testing.T) {
	pc, minor := 9, true // A minor = 12A
	from := CamelotFor(&Result{KeyPitchClass: &pc, KeyIsMinor: &minor})
	if from == nil || from.Code() != "12A" {
		t.Fatalf("from = %v", from)
	}
	pc2, minor2 := 2, true // D minor = 11A
	to := CamelotFor(&Result{KeyPitchClass: &pc2, KeyIsMinor: &minor2})
	if to == nil || to.Code() != "11A" {
		t.Fatalf("to = %v", to)
	}
	if score := HarmonicScore(from, to); score < 0.69 {
		t.Fatalf("harmonic score = %.2f", score)
	}
	keys := map[int]*CamelotKey{0: from, 1: to, 2: from}
	if chosen := PickHarmonicNext(1, []int{0, 1, 2}, from, keys); chosen != 0 && chosen != 2 {
		t.Fatalf("harmonic pick = %d", chosen)
	}
}

func TestServiceCacheAndPreparation(t *testing.T) {
	s := New(t.TempDir())
	if s.Cached("song") != nil {
		t.Fatal("empty cache returned a result")
	}
	s.RecordPreparation(900 * time.Millisecond)
	s.RecordPreparation(1100 * time.Millisecond)
	if lead := s.PreArmLead(3000); lead < 800 || lead > 2000 {
		t.Fatalf("lead = %d ms", lead)
	}
}

func intPtr(v int) *int { return &v }

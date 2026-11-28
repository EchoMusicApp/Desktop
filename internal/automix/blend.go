package automix

import "math"

// BlendDurationMs is 16 beats of the outgoing track, clamped to 4-15 s.
func BlendDurationMs(r *Result, beats, minMs, maxMs int) int {
	if r == nil || beats <= 0 {
		return minMs
	}
	return clampInt(int(math.Round(r.BeatPeriodMs()*float64(beats))), minMs, maxMs)
}

// TriggerMs is the remaining-time threshold that starts the crossfade so its
// end lands on a beat near the outgoing mix-out point.
func TriggerMs(durationMs, crossfadeMs int, r *Result, mixOutMs *int) int {
	if durationMs <= crossfadeMs || r == nil {
		return crossfadeMs
	}
	period := r.BeatPeriodMs()
	if period <= 0 {
		return crossfadeMs
	}
	target := clampInt(durationMs, crossfadeMs, durationMs)
	if mixOutMs != nil {
		target = clampInt(*mixOutMs, crossfadeMs, durationMs)
	}
	beatsBefore := math.Floor((float64(target) - r.FirstBeatOffsetMs) / period)
	aligned := r.FirstBeatOffsetMs + beatsBefore*period
	if aligned > float64(durationMs) {
		aligned -= period
	}
	start := aligned - float64(crossfadeMs)
	v := int(math.Round(float64(durationMs) - start))
	return clampInt(v, crossfadeMs, durationMs)
}

// AlignedStartMs offsets the incoming track so one of its beats lands at the
// end of the blend. This is the phase-matching part of Automix.
func AlignedStartMs(incoming *Result, crossfadeMs, maxSkipMs int) int {
	if incoming == nil {
		return 0
	}
	period := incoming.BeatPeriodMs()
	if period <= 0 {
		return 0
	}
	mixIn := 0
	if incoming.MixInMs != nil {
		mixIn = *incoming.MixInMs
	}
	start := incoming.FirstBeatOffsetMs - float64(crossfadeMs)
	if start < float64(mixIn) {
		beats := math.Ceil((float64(mixIn) - start) / period)
		start += beats * period
	}
	if start > float64(maxSkipMs) {
		return mixIn
	}
	return int(math.Max(0, start))
}

// PreArmLeadMs adapts the standby preparation lead from recent preparation
// durations: slow setups get a longer lead, fast ones a shorter one.
func PreArmLeadMs(recent []float64, fallbackMs int) int {
	if len(recent) == 0 {
		return fallbackMs
	}
	sum := 0.0
	for _, v := range recent {
		sum += v
	}
	average := sum / float64(len(recent))
	lead := average * 1.6
	if lead < 800 {
		lead = 800
	}
	if lead > 12000 {
		lead = 12000
	}
	return clampInt(int(lead), 800, 12000)
}

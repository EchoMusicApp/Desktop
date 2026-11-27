package stems

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// pcmWAV is the minimal PCM WAV description we need.
type pcmWAV struct {
	channels   int
	sampleRate uint32
	bits       int
	samples    []int16 // interleaved
}

func readPCM16WAV(path string) (pcmWAV, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return pcmWAV{}, err
	}
	if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return pcmWAV{}, fmt.Errorf("stems: %s is not a WAV file", filepath.Base(path))
	}
	r := bytes.NewReader(data[12:])
	var format string
	var channels, bits uint16
	var sampleRate uint32
	for r.Len() >= 8 {
		var id [4]byte
		var size uint32
		if _, err := r.Read(id[:]); err != nil {
			return pcmWAV{}, err
		}
		if err := binary.Read(r, binary.LittleEndian, &size); err != nil {
			return pcmWAV{}, err
		}
		chunk := make([]byte, size)
		if uint32(r.Len()) < size {
			return pcmWAV{}, errors.New("stems: truncated WAV chunk")
		}
		if _, err := r.Read(chunk); err != nil {
			return pcmWAV{}, err
		}
		switch string(id[:]) {
		case "fmt ":
			if len(chunk) < 16 {
				return pcmWAV{}, errors.New("stems: short WAV fmt chunk")
			}
			cr := bytes.NewReader(chunk)
			var audioFormat uint16
			_ = binary.Read(cr, binary.LittleEndian, &audioFormat)
			_ = binary.Read(cr, binary.LittleEndian, &channels)
			_ = binary.Read(cr, binary.LittleEndian, &sampleRate)
			_ = binary.Read(cr, binary.LittleEndian, new(uint32)) // byte rate
			_ = binary.Read(cr, binary.LittleEndian, new(uint16)) // block align
			_ = binary.Read(cr, binary.LittleEndian, &bits)
			format = string(id[:])
			if audioFormat != 1 || bits != 16 {
				return pcmWAV{}, fmt.Errorf("stems: unsupported WAV format audio=%d bits=%d", audioFormat, bits)
			}
		case "data":
			format = "data"
			if bits != 16 || channels <= 0 {
				return pcmWAV{}, errors.New("stems: WAV data before fmt chunk")
			}
			samples := make([]int16, len(chunk)/2)
			if err := binary.Read(bytes.NewReader(chunk), binary.LittleEndian, &samples); err != nil {
				return pcmWAV{}, err
			}
			return pcmWAV{channels: int(channels), sampleRate: sampleRate, bits: int(bits), samples: samples}, nil
		}
	}
	if format == "" {
		return pcmWAV{}, errors.New("stems: no WAV data chunk")
	}
	return pcmWAV{}, errors.New("stems: no WAV data chunk")
}

func writePCM16StereoWAV(path string, rate uint32, left, right []int16) error {
	if len(left) != len(right) {
		return errors.New("stems: mismatched channel lengths")
	}
	var out bytes.Buffer
	frames := len(left)
	dataSize := frames * 4
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(36+dataSize))
	out.WriteString("WAVEfmt ")
	_ = binary.Write(&out, binary.LittleEndian, uint32(16))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1)) // PCM
	_ = binary.Write(&out, binary.LittleEndian, uint16(2)) // stereo
	_ = binary.Write(&out, binary.LittleEndian, rate)
	_ = binary.Write(&out, binary.LittleEndian, rate*4)
	_ = binary.Write(&out, binary.LittleEndian, uint16(4))
	_ = binary.Write(&out, binary.LittleEndian, uint16(16))
	out.WriteString("data")
	_ = binary.Write(&out, binary.LittleEndian, uint32(dataSize))
	for i := range left {
		_ = binary.Write(&out, binary.LittleEndian, left[i])
		_ = binary.Write(&out, binary.LittleEndian, right[i])
	}
	return os.WriteFile(path, out.Bytes(), 0o644)
}

// Mix sums the enabled mono stems into a stereo WAV and returns its path.
// The name includes the enabled mask, so toggles reuse an existing mix.
func (s *Service) Mix(videoID string, enabled [4]bool) (string, error) {
	paths := s.Paths(videoID)
	var mask byte
	for i, on := range enabled {
		if on {
			mask |= 1 << uint(i)
		}
	}
	target := filepath.Join(s.root, fmt.Sprintf("mix-%s-%02x.wav", safeID(videoID), mask))
	if regularFile(target) {
		return target, nil
	}
	var stems [4]pcmWAV
	frames := -1
	for i, path := range paths {
		if !enabled[i] {
			continue
		}
		wav, err := readPCM16WAV(path)
		if err != nil {
			return "", err
		}
		if wav.channels != 1 {
			return "", fmt.Errorf("stems: %s is not mono", filepath.Base(path))
		}
		if frames < 0 {
			frames = len(wav.samples)
		} else if len(wav.samples) != frames {
			return "", errors.New("stems: stem lengths differ")
		}
		stems[i] = wav
	}
	if frames <= 0 {
		return "", errors.New("stems: all stems are disabled")
	}
	rate := uint32(0)
	for i := range stems {
		if enabled[i] {
			rate = stems[i].sampleRate
			break
		}
	}
	left := make([]int16, frames)
	right := make([]int16, frames)
	for i := 0; i < frames; i++ {
		var acc int32
		for k := 0; k < 4; k++ {
			if enabled[k] {
				acc += int32(stems[k].samples[i])
			}
		}
		if acc > 32767 {
			acc = 32767
		} else if acc < -32768 {
			acc = -32768
		}
		left[i], right[i] = int16(acc), int16(acc)
	}
	tmp := target + ".part"
	if err := writePCM16StereoWAV(tmp, rate, left, right); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return target, nil
}

// EnabledFromKeys maps D/B/I/V style keys to the four stem positions.
func EnabledFromKeys(keys string) [4]bool {
	var out [4]bool
	for _, key := range strings.ToUpper(keys) {
		switch key {
		case 'D':
			out[0] = true
		case 'B':
			out[1] = true
		case 'I':
			out[2] = true
		case 'V':
			out[3] = true
		}
	}
	return out
}

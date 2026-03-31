package dsp

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestGain(t *testing.T) {
	in := []float32{0.5, -0.5, 0.25, -0.25}
	out := Gain(in, 2.0)
	want := []float32{1.0, -1.0, 0.5, -0.5}
	for i := range out {
		if math.Abs(float64(out[i]-want[i])) > 1e-6 {
			t.Errorf("sample %d: got %f, want %f", i, out[i], want[i])
		}
	}
}

func TestGainClamps(t *testing.T) {
	in := []float32{0.8, -0.8}
	out := Gain(in, 2.0)
	if out[0] > 1.0 || out[1] < -1.0 {
		t.Errorf("expected clamped output, got %v", out)
	}
}

func TestEchoImpulse(t *testing.T) {
	sr := 48000
	ch := 1
	n := 10000
	in := make([]float32, n)
	in[0] = 1.0 // impulse

	out := Echo(in, sr, ch, 100, 0.5)

	// 100ms delay at 48kHz mono = 4800 samples
	delaySamples := 4800
	if out[0] != 1.0 {
		t.Errorf("original at 0: got %f, want 1.0", out[0])
	}
	if math.Abs(float64(out[delaySamples])-0.5) > 0.01 {
		t.Errorf("echo at %d: got %f, want ~0.5", delaySamples, out[delaySamples])
	}
}

func TestEchoNoOpOnZeroDelay(t *testing.T) {
	in := []float32{0.5, 0.3}
	out := Echo(in, 48000, 1, 0, 0.5)
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("expected no change with zero delay")
		}
	}
}

func TestReverbExtendsAudio(t *testing.T) {
	sr := 48000
	ch := 1
	in := make([]float32, sr) // 1 second
	in[0] = 1.0

	out := Reverb(in, sr, ch, 50, 0.6)

	// Output should be longer than input (2s reverb tail).
	if len(out) <= len(in) {
		t.Errorf("reverb should extend audio: got %d, want > %d", len(out), len(in))
	}

	// Tail should contain non-zero samples (reverb ringing).
	tailStart := len(in)
	hasEnergy := false
	for i := tailStart; i < len(out); i++ {
		if math.Abs(float64(out[i])) > 1e-6 {
			hasEnergy = true
			break
		}
	}
	if !hasEnergy {
		t.Error("reverb tail is silent")
	}
}

func TestFilterBandpassPreserves1kHz(t *testing.T) {
	sr := 48000
	ch := 1
	n := sr // 1 second

	// 1kHz sine should pass through 300-3400 Hz bandpass.
	in := make([]float32, n)
	for i := range n {
		in[i] = float32(math.Sin(2 * math.Pi * 1000 * float64(i) / float64(sr)))
	}

	out := Filter(in, sr, ch, "bandpass", 300, 3400)

	// Check RMS of second half (after filter settles).
	var rms float64
	for i := n / 2; i < n; i++ {
		rms += float64(out[i]) * float64(out[i])
	}
	rms = math.Sqrt(rms / float64(n/2))

	if rms < 0.3 {
		t.Errorf("1kHz through bandpass 300-3400: RMS %f too low", rms)
	}
}

func TestFilterBandpassAttenuates100Hz(t *testing.T) {
	sr := 48000
	ch := 1
	n := sr

	// 100Hz sine should be attenuated by 300-3400 Hz bandpass.
	in := make([]float32, n)
	for i := range n {
		in[i] = float32(math.Sin(2 * math.Pi * 100 * float64(i) / float64(sr)))
	}

	out := Filter(in, sr, ch, "bandpass", 300, 3400)

	var rms float64
	for i := n / 2; i < n; i++ {
		rms += float64(out[i]) * float64(out[i])
	}
	rms = math.Sqrt(rms / float64(n/2))

	if rms > 0.3 {
		t.Errorf("100Hz through bandpass 300-3400: RMS %f too high (should be attenuated)", rms)
	}
}

func TestSpeedDoubleHalvesLength(t *testing.T) {
	in := make([]float32, 20) // 10 stereo frames
	for i := range in {
		in[i] = float32(i)
	}

	out := Speed(in, 2, 2.0)

	// 2x speed: 10 frames -> 5 frames = 10 samples
	if len(out) != 10 {
		t.Errorf("2x speed: got %d samples, want 10", len(out))
	}
}

func TestSpeedHalfDoublesLength(t *testing.T) {
	in := make([]float32, 20)
	for i := range in {
		in[i] = float32(i)
	}

	out := Speed(in, 2, 0.5)

	// 0.5x speed: 10 frames -> 20 frames = 40 samples
	if len(out) != 40 {
		t.Errorf("0.5x speed: got %d samples, want 40", len(out))
	}
}

func TestPitchOctaveUp(t *testing.T) {
	in := make([]float32, 1000) // mono
	for i := range in {
		in[i] = float32(i) / 1000.0
	}

	out := Pitch(in, 1, 12) // +12 semitones = 2x ratio = half length

	if len(out) < 450 || len(out) > 550 {
		t.Errorf("+12 semitones: got %d samples, want ~500", len(out))
	}
}

func TestPitchOctaveDown(t *testing.T) {
	in := make([]float32, 1000)
	for i := range in {
		in[i] = float32(i) / 1000.0
	}

	out := Pitch(in, 1, -12) // -12 semitones = 0.5x ratio = double length

	if len(out) < 1900 || len(out) > 2100 {
		t.Errorf("-12 semitones: got %d samples, want ~2000", len(out))
	}
}

func TestScratchExtendsAudio(t *testing.T) {
	sr := 48000
	ch := 2
	dur := 2 // 2 seconds
	n := dur * sr * ch
	in := make([]float32, n)
	for i := range n {
		in[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i/ch) / float64(sr)))
	}

	out := Scratch(in, sr, ch)

	if len(out) <= len(in) {
		t.Errorf("scratch: output (%d samples) should be longer than input (%d)", len(out), len(in))
	}
}

func TestScratchPreservesOriginal(t *testing.T) {
	sr := 48000
	ch := 1
	dur := 1 // 1 second
	n := dur * sr * ch
	in := make([]float32, n)
	for i := range n {
		in[i] = float32(i) / float32(n)
	}

	out := Scratch(in, sr, ch)

	// First section should be the original clip verbatim.
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("scratch altered original at sample %d: got %f, want %f", i, out[i], in[i])
			break
		}
	}
}

func TestScratchTooShortReturnsInput(t *testing.T) {
	sr := 48000
	ch := 1
	in := make([]float32, sr/4) // 0.25s -- too short

	out := Scratch(in, sr, ch)

	if len(out) != len(in) {
		t.Errorf("short clip: expected passthrough, got len %d vs %d", len(out), len(in))
	}
}

// TestReadWAVRoundTrip writes a WAV via clip.SaveWAV-style code, then reads it
// back with ReadWAV and checks the samples match.
func TestReadWAVRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wav")

	// Generate a short 16-bit PCM WAV file.
	sr := 48000
	ch := 2
	numSamples := 960 // 10ms stereo
	samples := make([]float32, numSamples)
	for i := range numSamples {
		samples[i] = float32(math.Sin(2*math.Pi*440*float64(i/ch)/float64(sr))) * 0.8
	}

	// Write WAV (same logic as clip.SaveWAV).
	bitsPerSample := 16
	byteRate := sr * ch * bitsPerSample / 8
	blockAlign := ch * bitsPerSample / 8
	dataSize := numSamples * 2

	buf := make([]byte, 44+dataSize)
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+dataSize))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16)
	binary.LittleEndian.PutUint16(buf[20:22], 1)
	binary.LittleEndian.PutUint16(buf[22:24], uint16(ch))
	binary.LittleEndian.PutUint32(buf[24:28], uint32(sr))
	binary.LittleEndian.PutUint32(buf[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(buf[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(buf[34:36], uint16(bitsPerSample))
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataSize))

	for i, s := range samples {
		if s > 1.0 {
			s = 1.0
		} else if s < -1.0 {
			s = -1.0
		}
		binary.LittleEndian.PutUint16(buf[44+i*2:], uint16(int16(s*math.MaxInt16)))
	}

	if err := os.WriteFile(path, buf, 0644); err != nil {
		t.Fatalf("write WAV: %v", err)
	}

	// Read it back.
	got, gotSR, gotCh, err := ReadWAV(path)
	if err != nil {
		t.Fatalf("ReadWAV: %v", err)
	}

	if gotSR != sr {
		t.Errorf("sample rate: got %d, want %d", gotSR, sr)
	}
	if gotCh != ch {
		t.Errorf("channels: got %d, want %d", gotCh, ch)
	}
	if len(got) != numSamples {
		t.Fatalf("sample count: got %d, want %d", len(got), numSamples)
	}

	// 16-bit quantization means we lose some precision. Allow ~1/32768 error.
	maxErr := float64(1.0 / math.MaxInt16)
	for i := range got {
		diff := math.Abs(float64(got[i] - samples[i]))
		if diff > maxErr*2 {
			t.Errorf("sample %d: got %f, want %f (diff %f)", i, got[i], samples[i], diff)
			break
		}
	}
}

func TestConvertChannelsMonoToStereo(t *testing.T) {
	mono := []float32{0.5, -0.5, 0.3}
	stereo := convertChannels(mono, 1, 2)

	if len(stereo) != 6 {
		t.Fatalf("expected 6 samples, got %d", len(stereo))
	}
	// Each mono sample should appear in both channels.
	for i, m := range mono {
		if stereo[i*2] != m || stereo[i*2+1] != m {
			t.Errorf("frame %d: got [%f, %f], want [%f, %f]", i, stereo[i*2], stereo[i*2+1], m, m)
		}
	}
}

func TestConvertChannelsStereoToMono(t *testing.T) {
	stereo := []float32{0.4, 0.6, -0.2, -0.8}
	mono := convertChannels(stereo, 2, 1)

	if len(mono) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(mono))
	}
	if math.Abs(float64(mono[0]-0.5)) > 1e-6 {
		t.Errorf("frame 0: got %f, want 0.5", mono[0])
	}
	if math.Abs(float64(mono[1]-(-0.5))) > 1e-6 {
		t.Errorf("frame 1: got %f, want -0.5", mono[1])
	}
}

func TestResampleIdentity(t *testing.T) {
	in := []float32{0.1, 0.2, 0.3, 0.4, 0.5, 0.6}
	out := resample(in, 2, 1.0)

	if len(out) != len(in) {
		t.Fatalf("1x resample: got %d samples, want %d", len(out), len(in))
	}
	for i := range in {
		if math.Abs(float64(out[i]-in[i])) > 1e-5 {
			t.Errorf("sample %d: got %f, want %f", i, out[i], in[i])
		}
	}
}

func TestClamp(t *testing.T) {
	s := []float32{1.5, -1.5, 0.5, -0.5}
	clamp(s)
	if s[0] != 1.0 || s[1] != -1.0 || s[2] != 0.5 || s[3] != -0.5 {
		t.Errorf("clamp: got %v", s)
	}
}

// --- Bug fix tests ---

func TestEchoTailExtends(t *testing.T) {
	sr := 48000
	ch := 1
	// 1-second clip with energy at the very end.
	in := make([]float32, sr)
	in[sr-1] = 1.0

	out := Echo(in, sr, ch, 200, 0.5)

	// Output must be longer than input to hold the echo tail.
	if len(out) <= len(in) {
		t.Fatalf("echo should extend audio: got %d, want > %d", len(out), len(in))
	}

	// The echo of the last sample (at 200ms after clip end) should be present.
	echoIdx := sr - 1 + 200*sr/1000
	if echoIdx >= len(out) {
		t.Fatalf("echo tail too short to contain first echo at index %d (len %d)", echoIdx, len(out))
	}
	if math.Abs(float64(out[echoIdx])-0.5) > 0.01 {
		t.Errorf("echo of last sample: got %f at index %d, want ~0.5", out[echoIdx], echoIdx)
	}
}

func TestEchoTailCappedAt5Seconds(t *testing.T) {
	sr := 48000
	ch := 1
	in := make([]float32, sr) // 1 second
	in[0] = 1.0

	// Very high decay = long tail, but capped at 5 seconds.
	out := Echo(in, sr, ch, 100, 0.95)

	maxLen := sr + 5*sr // 1s input + 5s tail
	if len(out) > maxLen {
		t.Errorf("echo tail exceeded 5s cap: got %d samples, want <= %d", len(out), maxLen)
	}
	if len(out) <= sr {
		t.Errorf("echo tail should extend audio: got %d", len(out))
	}
}

func TestReadWAVExtensible(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "extensible.wav")

	sr := 48000
	ch := 2
	numSamples := 100
	bitsPerSample := 16
	dataSize := numSamples * 2

	// Build a WAVE_FORMAT_EXTENSIBLE WAV file.
	// fmt chunk is 40 bytes for EXTENSIBLE.
	fmtSize := 40
	headerSize := 12 + 8 + fmtSize + 8 // RIFF(12) + fmt hdr(8) + fmt(40) + data hdr(8)
	buf := make([]byte, headerSize+dataSize)

	// RIFF header
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(headerSize-8+dataSize))
	copy(buf[8:12], "WAVE")

	// fmt chunk
	off := 12
	copy(buf[off:off+4], "fmt ")
	binary.LittleEndian.PutUint32(buf[off+4:off+8], uint32(fmtSize))
	binary.LittleEndian.PutUint16(buf[off+8:off+10], 0xFFFE)       // EXTENSIBLE
	binary.LittleEndian.PutUint16(buf[off+10:off+12], uint16(ch))   // channels
	binary.LittleEndian.PutUint32(buf[off+12:off+16], uint32(sr))   // sample rate
	byteRate := sr * ch * bitsPerSample / 8
	binary.LittleEndian.PutUint32(buf[off+16:off+20], uint32(byteRate))
	blockAlign := ch * bitsPerSample / 8
	binary.LittleEndian.PutUint16(buf[off+20:off+22], uint16(blockAlign))
	binary.LittleEndian.PutUint16(buf[off+22:off+24], uint16(bitsPerSample))
	binary.LittleEndian.PutUint16(buf[off+24:off+26], 22)           // cbSize
	binary.LittleEndian.PutUint16(buf[off+26:off+28], uint16(bitsPerSample)) // valid bits
	binary.LittleEndian.PutUint32(buf[off+28:off+32], 3)            // channel mask (FL|FR)
	// SubFormat GUID for PCM: {00000001-0000-0010-8000-00aa00389b71}
	pcmGUID := []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00,
		0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}
	copy(buf[off+32:off+48], pcmGUID)

	// data chunk
	off = 12 + 8 + fmtSize
	copy(buf[off:off+4], "data")
	binary.LittleEndian.PutUint32(buf[off+4:off+8], uint32(dataSize))

	// Write known samples.
	dataOff := off + 8
	for i := range numSamples {
		binary.LittleEndian.PutUint16(buf[dataOff+i*2:], uint16(int16(1000+i)))
	}

	if err := os.WriteFile(path, buf, 0644); err != nil {
		t.Fatalf("write WAV: %v", err)
	}

	// Read it back.
	got, gotSR, gotCh, err := ReadWAV(path)
	if err != nil {
		t.Fatalf("ReadWAV failed on EXTENSIBLE: %v", err)
	}
	if gotSR != sr {
		t.Errorf("sample rate: got %d, want %d", gotSR, sr)
	}
	if gotCh != ch {
		t.Errorf("channels: got %d, want %d", gotCh, ch)
	}
	if len(got) != numSamples {
		t.Fatalf("sample count: got %d, want %d", len(got), numSamples)
	}

	// Verify first sample value.
	expected := float32(1000) / float32(math.MaxInt16)
	if math.Abs(float64(got[0]-expected)) > 1e-4 {
		t.Errorf("sample 0: got %f, want %f", got[0], expected)
	}
}

func TestReverbStereoWidth(t *testing.T) {
	sr := 48000
	ch := 2
	// 0.5s stereo impulse: energy only in left channel.
	n := sr / 2 * ch
	in := make([]float32, n)
	in[0] = 1.0 // left channel impulse

	out := Reverb(in, sr, ch, 50, 0.6)

	// After reverb, both channels should have energy (stereo width means
	// different delays, so L and R reverb tails should differ).
	// Check that L and R are NOT identical in the reverb tail.
	tailStart := len(in)
	identical := true
	for i := tailStart; i < len(out)-1; i += 2 {
		if out[i] != out[i+1] {
			identical = false
			break
		}
	}

	// With a mono impulse (L=1, R=0), different per-channel delays
	// guarantee L and R outputs diverge.
	if identical {
		t.Error("reverb L and R channels are identical -- no stereo width")
	}
}

func TestScratchFadeOut(t *testing.T) {
	sr := 48000
	ch := 2
	dur := 2
	n := dur * sr * ch
	in := make([]float32, n)
	// Fill with a constant so any hard cutoff is obvious.
	for i := range n {
		in[i] = 0.5
	}

	out := Scratch(in, sr, ch)

	// Last stereo frame should be near zero (faded out).
	lastL := out[len(out)-2]
	lastR := out[len(out)-1]
	if math.Abs(float64(lastL)) > 0.01 || math.Abs(float64(lastR)) > 0.01 {
		t.Errorf("scratch ends abruptly: last frame = [%f, %f], want near zero", lastL, lastR)
	}
}

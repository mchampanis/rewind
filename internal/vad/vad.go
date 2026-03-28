// Package vad implements voice activity detection using the Silero VAD model
// via ONNX Runtime. It detects speech segments in audio clips for auto-trimming.
package vad

import (
	"fmt"

	ort "github.com/yalue/onnxruntime_go"
)

const (
	// Silero VAD v5 expects 16kHz mono input in 512-sample frames (32ms each).
	vadRate      = 16000
	vadFrameSize = 512
	// LSTM state dimensions: (2, 1, 64).
	stateD0 = 2
	stateD1 = 1
	stateD2 = 64
)

// Segment represents a time range of detected speech.
type Segment struct {
	Start float64 // seconds
	End   float64 // seconds
}

// Detector wraps a Silero VAD ONNX session.
type Detector struct {
	session *ort.AdvancedSession
	// Pre-allocated tensors reused across Run() calls.
	inputTensor  *ort.Tensor[float32]
	h0           *ort.Tensor[float32]
	c0           *ort.Tensor[float32]
	outputTensor *ort.Tensor[float32]
	hn           *ort.Tensor[float32]
	cn           *ort.Tensor[float32]
}

// InitRuntime initializes the ONNX Runtime environment. Call once at startup.
func InitRuntime() error {
	if ort.IsInitialized() {
		return nil
	}
	return ort.InitializeEnvironment()
}

// DestroyRuntime tears down the ONNX Runtime environment. Call once at shutdown.
func DestroyRuntime() error {
	if !ort.IsInitialized() {
		return nil
	}
	return ort.DestroyEnvironment()
}

// NewDetector creates a VAD detector from the embedded model.
func NewDetector() (*Detector, error) {
	if !ort.IsInitialized() {
		return nil, fmt.Errorf("ONNX runtime not initialized (call vad.InitRuntime first)")
	}

	inputTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(1, vadFrameSize))
	if err != nil {
		return nil, fmt.Errorf("create input tensor: %w", err)
	}
	h0, err := ort.NewEmptyTensor[float32](ort.NewShape(stateD0, stateD1, stateD2))
	if err != nil {
		inputTensor.Destroy()
		return nil, fmt.Errorf("create h0 tensor: %w", err)
	}
	c0, err := ort.NewEmptyTensor[float32](ort.NewShape(stateD0, stateD1, stateD2))
	if err != nil {
		inputTensor.Destroy()
		h0.Destroy()
		return nil, fmt.Errorf("create c0 tensor: %w", err)
	}
	outputTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 1))
	if err != nil {
		inputTensor.Destroy()
		h0.Destroy()
		c0.Destroy()
		return nil, fmt.Errorf("create output tensor: %w", err)
	}
	hn, err := ort.NewEmptyTensor[float32](ort.NewShape(stateD0, stateD1, stateD2))
	if err != nil {
		inputTensor.Destroy()
		h0.Destroy()
		c0.Destroy()
		outputTensor.Destroy()
		return nil, fmt.Errorf("create hn tensor: %w", err)
	}
	cn, err := ort.NewEmptyTensor[float32](ort.NewShape(stateD0, stateD1, stateD2))
	if err != nil {
		inputTensor.Destroy()
		h0.Destroy()
		c0.Destroy()
		outputTensor.Destroy()
		hn.Destroy()
		return nil, fmt.Errorf("create cn tensor: %w", err)
	}

	inputs := []ort.Value{inputTensor, h0, c0}
	outputs := []ort.Value{outputTensor, hn, cn}
	inputNames := []string{"input", "h0", "c0"}
	outputNames := []string{"output", "hn", "cn"}

	session, err := ort.NewAdvancedSessionWithONNXData(
		modelData, inputNames, outputNames, inputs, outputs, nil,
	)
	if err != nil {
		inputTensor.Destroy()
		h0.Destroy()
		c0.Destroy()
		outputTensor.Destroy()
		hn.Destroy()
		cn.Destroy()
		return nil, fmt.Errorf("create ONNX session: %w", err)
	}

	return &Detector{
		session:      session,
		inputTensor:  inputTensor,
		h0:           h0,
		c0:           c0,
		outputTensor: outputTensor,
		hn:           hn,
		cn:           cn,
	}, nil
}

// Close releases the ONNX session and tensors.
func (d *Detector) Close() {
	if d.session != nil {
		d.session.Destroy()
	}
	d.inputTensor.Destroy()
	d.h0.Destroy()
	d.c0.Destroy()
	d.outputTensor.Destroy()
	d.hn.Destroy()
	d.cn.Destroy()
}

// DetectSpeech runs VAD on the given audio and returns time segments where
// speech was detected above the given probability threshold.
// Input samples are interleaved float32 at the given sample rate and channel count.
func (d *Detector) DetectSpeech(samples []float32, sampleRate, channels int, threshold float64) ([]Segment, error) {
	// Downsample to 16kHz mono for the VAD model.
	mono := stereoToMono(samples, channels)
	mono16k := downsample(mono, sampleRate, vadRate)

	// Reset LSTM state to zeros.
	d.zeroState()

	// Seconds per VAD frame in the original audio timeline.
	frameDuration := float64(vadFrameSize) / float64(vadRate)

	// Process in 512-sample frames, collecting per-frame probabilities.
	numFrames := len(mono16k) / vadFrameSize
	probs := make([]float64, numFrames)
	inputData := d.inputTensor.GetData()

	for i := range numFrames {
		copy(inputData, mono16k[i*vadFrameSize:(i+1)*vadFrameSize])

		if err := d.session.Run(); err != nil {
			return nil, fmt.Errorf("VAD inference frame %d: %w", i, err)
		}

		probs[i] = float64(d.outputTensor.GetData()[0])

		// Feed LSTM state forward: hn -> h0, cn -> c0.
		copy(d.h0.GetData(), d.hn.GetData())
		copy(d.c0.GetData(), d.cn.GetData())
	}

	// Group voiced frames into segments with padding and merging.
	return buildSegments(probs, frameDuration, threshold), nil
}

// zeroState resets the LSTM hidden and cell state tensors to zero.
func (d *Detector) zeroState() {
	d.h0.ZeroContents()
	d.c0.ZeroContents()
}

const (
	// Padding added around each detected speech segment.
	segmentPadding = 0.1 // seconds
	// Maximum gap between speech segments before they are merged.
	mergeGap = 0.3 // seconds
)

// buildSegments groups consecutive voiced frames into time segments.
func buildSegments(probs []float64, frameDuration, threshold float64) []Segment {
	var raw []Segment
	inSpeech := false
	var start float64

	for i, p := range probs {
		t := float64(i) * frameDuration
		if p >= threshold && !inSpeech {
			start = t
			inSpeech = true
		} else if p < threshold && inSpeech {
			raw = append(raw, Segment{Start: start, End: t + frameDuration})
			inSpeech = false
		}
	}
	if inSpeech {
		raw = append(raw, Segment{Start: start, End: float64(len(probs)) * frameDuration})
	}

	if len(raw) == 0 {
		return nil
	}

	// Add padding.
	for i := range raw {
		raw[i].Start -= segmentPadding
		if raw[i].Start < 0 {
			raw[i].Start = 0
		}
		raw[i].End += segmentPadding
	}

	// Merge segments that are close together.
	merged := []Segment{raw[0]}
	for _, seg := range raw[1:] {
		last := &merged[len(merged)-1]
		if seg.Start-last.End <= mergeGap {
			last.End = seg.End
		} else {
			merged = append(merged, seg)
		}
	}

	return merged
}

// TrimToSegments extracts the audio corresponding to the given segments from
// interleaved samples at the given sample rate and channel count.
// Returns the trimmed samples as a contiguous slice.
func TrimToSegments(samples []float32, sampleRate, channels int, segments []Segment) []float32 {
	var out []float32
	totalFrames := len(samples) / channels
	totalDuration := float64(totalFrames) / float64(sampleRate)

	for _, seg := range segments {
		startFrame := int(seg.Start * float64(sampleRate))
		endFrame := int(seg.End * float64(sampleRate))
		if startFrame < 0 {
			startFrame = 0
		}
		if endFrame > totalFrames {
			endFrame = totalFrames
		}
		if seg.End > totalDuration {
			endFrame = totalFrames
		}
		if startFrame >= endFrame {
			continue
		}
		out = append(out, samples[startFrame*channels:endFrame*channels]...)
	}
	return out
}

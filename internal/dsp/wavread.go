package dsp

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

// ReadWAV loads a WAV file and returns interleaved float32 samples,
// sample rate, and channel count. Supports PCM 16-bit, 24-bit, IEEE float
// 32-bit, and WAVE_FORMAT_EXTENSIBLE containers wrapping those formats.
func ReadWAV(path string) (samples []float32, sampleRate int, channels int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	defer f.Close()

	// Read and validate RIFF/WAVE header.
	var riffHeader [12]byte
	if _, err := io.ReadFull(f, riffHeader[:]); err != nil {
		return nil, 0, 0, fmt.Errorf("read RIFF header: %w", err)
	}
	if string(riffHeader[0:4]) != "RIFF" || string(riffHeader[8:12]) != "WAVE" {
		return nil, 0, 0, fmt.Errorf("not a WAV file")
	}

	// Walk chunks to find "fmt " and "data".
	var format uint16
	var bitsPerSample int
	var dataBytes []byte
	fmtFound := false

	// Track total bytes read per chunk so we can skip pad bytes.
chunks:
	for {
		var chunkHeader [8]byte
		if _, err := io.ReadFull(f, chunkHeader[:]); err != nil {
			break
		}
		chunkID := string(chunkHeader[0:4])
		chunkSize := int(binary.LittleEndian.Uint32(chunkHeader[4:8]))

		switch chunkID {
		case "fmt ":
			if chunkSize < 16 {
				return nil, 0, 0, fmt.Errorf("fmt chunk too small: %d", chunkSize)
			}
			fmtData := make([]byte, chunkSize)
			if _, err := io.ReadFull(f, fmtData); err != nil {
				return nil, 0, 0, fmt.Errorf("read fmt chunk: %w", err)
			}
			format = binary.LittleEndian.Uint16(fmtData[0:2])
			channels = int(binary.LittleEndian.Uint16(fmtData[2:4]))
			sampleRate = int(binary.LittleEndian.Uint32(fmtData[4:8]))
			bitsPerSample = int(binary.LittleEndian.Uint16(fmtData[14:16]))

			// WAVE_FORMAT_EXTENSIBLE: real format tag is in the SubFormat GUID.
			if format == 0xFFFE && chunkSize >= 40 {
				format = binary.LittleEndian.Uint16(fmtData[24:26])
			}

			fmtFound = true
			// Pad to even boundary.
			if chunkSize%2 != 0 {
				io.CopyN(io.Discard, f, 1)
			}

		case "data":
			dataBytes = make([]byte, chunkSize)
			if _, err := io.ReadFull(f, dataBytes); err != nil {
				return nil, 0, 0, fmt.Errorf("read data chunk: %w", err)
			}
			if chunkSize%2 != 0 {
				io.CopyN(io.Discard, f, 1)
			}

		default:
			// Skip unknown chunks (pad to even boundary).
			skip := int64(chunkSize)
			if chunkSize%2 != 0 {
				skip++
			}
			if _, err := io.CopyN(io.Discard, f, skip); err != nil {
				break chunks
			}
		}
	}

	if !fmtFound {
		return nil, 0, 0, fmt.Errorf("no fmt chunk found")
	}
	if dataBytes == nil {
		return nil, 0, 0, fmt.Errorf("no data chunk found")
	}
	if format != 1 && format != 3 {
		return nil, 0, 0, fmt.Errorf("unsupported WAV format tag: %d (want PCM=1 or float=3)", format)
	}

	bytesPerSample := bitsPerSample / 8
	if bytesPerSample <= 0 {
		return nil, 0, 0, fmt.Errorf("invalid bits per sample: %d", bitsPerSample)
	}
	numSamples := len(dataBytes) / bytesPerSample
	samples = make([]float32, numSamples)

	switch {
	case format == 3 && bitsPerSample == 32:
		for i := range numSamples {
			bits := binary.LittleEndian.Uint32(dataBytes[i*4:])
			samples[i] = math.Float32frombits(bits)
		}
	case format == 1 && bitsPerSample == 16:
		for i := range numSamples {
			v := int16(binary.LittleEndian.Uint16(dataBytes[i*2:]))
			samples[i] = float32(v) / float32(math.MaxInt16)
		}
	case format == 1 && bitsPerSample == 24:
		for i := range numSamples {
			b := dataBytes[i*3 : i*3+3]
			v := int32(b[0]) | int32(b[1])<<8 | int32(b[2])<<16
			if v&0x800000 != 0 {
				v |= ^0xFFFFFF // sign extend
			}
			samples[i] = float32(v) / float32(1<<23)
		}
	default:
		return nil, 0, 0, fmt.Errorf("unsupported: %d-bit format=%d", bitsPerSample, format)
	}

	return samples, sampleRate, channels, nil
}

package audio

import (
	"fmt"
	"log"
	"math"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

// Player renders audio samples to a WASAPI render endpoint.
type Player struct {
	mu      sync.Mutex
	playing bool
	stopCh  chan struct{}
	doneCh  chan struct{}
	device  string // device name or "" for default
}

// NewPlayer creates a player targeting the named render device.
func NewPlayer(deviceName string) *Player {
	return &Player{device: deviceName}
}

// Play starts rendering the given samples in a background goroutine.
// Returns an error if playback is already in progress.
func (p *Player) Play(samples []float32, sampleRate, channels int) error {
	p.mu.Lock()
	if p.playing {
		p.mu.Unlock()
		return fmt.Errorf("playback already in progress")
	}
	p.stopCh = make(chan struct{})
	p.doneCh = make(chan struct{})
	p.playing = true
	p.mu.Unlock()

	go p.renderLoop(samples, sampleRate, channels)
	return nil
}

// Stop cancels playback and waits for the render goroutine to exit.
func (p *Player) Stop() {
	p.mu.Lock()
	if !p.playing {
		p.mu.Unlock()
		return
	}
	close(p.stopCh)
	p.mu.Unlock()

	<-p.doneCh

	p.mu.Lock()
	p.playing = false
	p.mu.Unlock()
}

// Playing reports whether playback is active.
func (p *Player) Playing() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing
}

// renderLoop runs on a dedicated goroutine, locked to an OS thread for COM.
func (p *Player) renderLoop(samples []float32, sampleRate, channels int) {
	defer func() {
		p.mu.Lock()
		p.playing = false
		p.mu.Unlock()
		close(p.doneCh)
	}()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := comInit(); err != nil {
		log.Printf("render: COM init: %v", err)
		return
	}
	defer ole.CoUninitialize()

	// Find the render device.
	var dev *wca.IMMDevice
	var err error
	if p.device == "" {
		dev, err = getDefaultRenderDevice()
	} else {
		dev, err = findDevice(p.device, wca.ERender)
	}
	if err != nil {
		log.Printf("render: find device: %v", err)
		return
	}
	defer dev.Release()

	name, _ := deviceFriendlyName(dev)
	log.Printf("render: using device %q", name)

	// Activate IAudioClient.
	var ac *wca.IAudioClient
	if err := dev.Activate(wca.IID_IAudioClient, wca.CLSCTX_ALL, nil, &ac); err != nil {
		log.Printf("render: activate audio client: %v", err)
		return
	}
	defer ac.Release()

	// Get the device mix format.
	var wfx *wca.WAVEFORMATEX
	if err := ac.GetMixFormat(&wfx); err != nil {
		log.Printf("render: get mix format: %v", err)
		return
	}
	defer ole.CoTaskMemFree(uintptr(unsafe.Pointer(wfx)))

	log.Printf("render: mix format: %d Hz, %d ch, %d bits, tag=0x%X",
		wfx.NSamplesPerSec, wfx.NChannels, wfx.WBitsPerSample, wfx.WFormatTag)

	// Get device period.
	var defaultPeriod, minPeriod wca.REFERENCE_TIME
	if err := ac.GetDevicePeriod(&defaultPeriod, &minPeriod); err != nil {
		log.Printf("render: get device period: %v", err)
		return
	}

	// Initialize in shared mode with event-driven buffering.
	streamFlags := uint32(wca.AUDCLNT_STREAMFLAGS_EVENTCALLBACK)
	if err := ac.Initialize(
		wca.AUDCLNT_SHAREMODE_SHARED,
		streamFlags,
		defaultPeriod,
		0,
		wfx,
		nil,
	); err != nil {
		log.Printf("render: initialize: %v", err)
		return
	}

	// Create an event handle for buffer notifications.
	event, err := createEvent()
	if err != nil {
		log.Printf("render: create event: %v", err)
		return
	}
	defer closeHandle(event)

	if err := ac.SetEventHandle(event); err != nil {
		log.Printf("render: set event handle: %v", err)
		return
	}

	// Get the render client.
	var arc *wca.IAudioRenderClient
	if err := ac.GetService(wca.IID_IAudioRenderClient, &arc); err != nil {
		log.Printf("render: get render client: %v", err)
		return
	}
	defer arc.Release()

	// Get buffer size.
	var bufferFrames uint32
	if err := ac.GetBufferSize(&bufferFrames); err != nil {
		log.Printf("render: get buffer size: %v", err)
		return
	}
	log.Printf("render: buffer %d frames (%.1f ms)", bufferFrames,
		float64(bufferFrames)/float64(wfx.NSamplesPerSec)*1000)

	devChannels := int(wfx.NChannels)
	devBits := int(wfx.WBitsPerSample)
	devFloat := wfx.WFormatTag == waveFormatIEEEFloat || isExtensibleFloat(wfx)
	devRate := int(wfx.NSamplesPerSec)

	if sampleRate != devRate {
		log.Printf("render: clip sample rate %d Hz != device %d Hz, playback speed will differ",
			sampleRate, devRate)
	}
	if channels != devChannels {
		log.Printf("render: clip channels %d != device %d, channel layout will differ",
			channels, devChannels)
	}

	// Start the audio stream.
	if err := ac.Start(); err != nil {
		log.Printf("render: start: %v", err)
		return
	}
	defer ac.Stop()

	log.Printf("render: playing %d samples (%.1fs)", len(samples),
		float64(len(samples))/float64(sampleRate)/float64(channels))

	// cursor tracks how far through the source samples we've written.
	cursor := 0
	totalFrames := len(samples) / channels
	done := false

	for !done {
		r := waitForSingleObject(event, 200)
		select {
		case <-p.stopCh:
			log.Print("render: stopped by user")
			return
		default:
		}

		if r != 0 && r != 258 { // 258 = WAIT_TIMEOUT
			log.Printf("render: wait error: %d", r)
			time.Sleep(10 * time.Millisecond)
			continue
		}

		// How many frames can we write?
		var padding uint32
		if err := ac.GetCurrentPadding(&padding); err != nil {
			log.Printf("render: get padding: %v", err)
			break
		}
		available := int(bufferFrames - padding)
		if available <= 0 {
			continue
		}

		// Don't write more frames than we have left.
		remaining := totalFrames - cursor
		if available > remaining {
			available = remaining
		}
		if available == 0 {
			// All samples written; wait for buffer to drain.
			if padding == 0 {
				done = true
			}
			continue
		}

		var data *byte
		if err := arc.GetBuffer(uint32(available), &data); err != nil {
			log.Printf("render: get buffer: %v", err)
			break
		}

		// Write samples into the render buffer.
		srcStart := cursor * channels
		srcEnd := srcStart + available*channels
		writeFloat32ToDevice(data, samples[srcStart:srcEnd], devBits, devFloat)
		cursor += available

		if err := arc.ReleaseBuffer(uint32(available), 0); err != nil {
			log.Printf("render: release buffer: %v", err)
			break
		}
	}

	log.Print("render: playback finished")
}

// writeFloat32ToDevice converts float32 samples to the device's native format
// and writes them into the WASAPI render buffer.
func writeFloat32ToDevice(dst *byte, samples []float32, bitsPerSample int, isFloat bool) {
	totalSamples := len(samples)

	if isFloat && bitsPerSample == 32 {
		out := unsafe.Slice((*float32)(unsafe.Pointer(dst)), totalSamples)
		copy(out, samples)
		return
	}

	if bitsPerSample == 16 {
		out := unsafe.Slice((*int16)(unsafe.Pointer(dst)), totalSamples)
		for i, s := range samples {
			if s > 1.0 {
				s = 1.0
			} else if s < -1.0 {
				s = -1.0
			}
			out[i] = int16(s * float32(math.MaxInt16))
		}
		return
	}

	if bitsPerSample == 24 {
		out := unsafe.Slice(dst, totalSamples*3)
		for i, s := range samples {
			if s > 1.0 {
				s = 1.0
			} else if s < -1.0 {
				s = -1.0
			}
			val := int32(s * float32(1<<23))
			offset := i * 3
			out[offset] = byte(val)
			out[offset+1] = byte(val >> 8)
			out[offset+2] = byte(val >> 16)
		}
		return
	}

	log.Printf("render: unsupported device format: %d-bit isFloat=%v, writing silence", bitsPerSample, isFloat)
}

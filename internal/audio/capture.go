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

	"rewind/internal/buffer"
)

// WAVE_FORMAT_IEEE_FLOAT is the format tag for 32-bit float PCM.
const waveFormatIEEEFloat = 0x0003

// Capturer performs WASAPI loopback capture from a render endpoint and writes
// the captured audio into a ring buffer.
type Capturer struct {
	mu       sync.Mutex
	running  bool
	stopCh   chan struct{}
	doneCh   chan struct{}
	ring     *buffer.Ring
	device   string // device name or "" for default
}

// NewCapturer creates a capturer that will write into the given ring buffer.
func NewCapturer(ring *buffer.Ring, deviceName string) *Capturer {
	return &Capturer{
		ring:   ring,
		device: deviceName,
	}
}

// Start begins loopback capture in a background goroutine.
func (c *Capturer) Start() error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("capture already running")
	}
	c.stopCh = make(chan struct{})
	c.doneCh = make(chan struct{})
	c.running = true
	c.mu.Unlock()

	go c.captureLoop()
	return nil
}

// Stop signals the capture goroutine to exit and waits for it.
func (c *Capturer) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	close(c.stopCh)
	c.mu.Unlock()

	<-c.doneCh

	c.mu.Lock()
	c.running = false
	c.mu.Unlock()
}

// Running reports whether capture is active.
func (c *Capturer) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// captureLoop runs on a dedicated goroutine, locked to an OS thread for COM.
func (c *Capturer) captureLoop() {
	defer close(c.doneCh)

	// Lock this goroutine to an OS thread for COM.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := comInit(); err != nil {
		log.Printf("capture: COM init: %v", err)
		return
	}
	defer ole.CoUninitialize()

	// Find the render device to loopback-capture from.
	var dev *wca.IMMDevice
	var err error
	if c.device == "" {
		dev, err = getDefaultRenderDevice()
	} else {
		dev, err = findDevice(c.device, wca.ERender)
	}
	if err != nil {
		log.Printf("capture: find device: %v", err)
		return
	}
	defer dev.Release()

	name, _ := deviceFriendlyName(dev)
	log.Printf("capture: using device %q", name)

	// Activate IAudioClient on the render device.
	var ac *wca.IAudioClient
	if err := dev.Activate(wca.IID_IAudioClient, wca.CLSCTX_ALL, nil, &ac); err != nil {
		log.Printf("capture: activate audio client: %v", err)
		return
	}
	defer ac.Release()

	// Get the device's mix format.
	var wfx *wca.WAVEFORMATEX
	if err := ac.GetMixFormat(&wfx); err != nil {
		log.Printf("capture: get mix format: %v", err)
		return
	}
	defer ole.CoTaskMemFree(uintptr(unsafe.Pointer(wfx)))

	log.Printf("capture: mix format: %d Hz, %d ch, %d bits, tag=0x%X",
		wfx.NSamplesPerSec, wfx.NChannels, wfx.WBitsPerSample, wfx.WFormatTag)

	// Initialize in shared loopback mode with event-driven buffering.
	// AUDCLNT_STREAMFLAGS_LOOPBACK captures what the render device is playing.
	// AUDCLNT_STREAMFLAGS_EVENTCALLBACK lets us use WaitForSingleObject.
	var defaultPeriod, minPeriod wca.REFERENCE_TIME
	if err := ac.GetDevicePeriod(&defaultPeriod, &minPeriod); err != nil {
		log.Printf("capture: get device period: %v", err)
		return
	}

	streamFlags := uint32(wca.AUDCLNT_STREAMFLAGS_LOOPBACK | wca.AUDCLNT_STREAMFLAGS_EVENTCALLBACK)
	if err := ac.Initialize(
		wca.AUDCLNT_SHAREMODE_SHARED,
		streamFlags,
		defaultPeriod,
		0,
		wfx,
		nil,
	); err != nil {
		log.Printf("capture: initialize: %v", err)
		return
	}

	// Create an event handle for buffer notifications.
	event, err := createEvent()
	if err != nil {
		log.Printf("capture: create event: %v", err)
		return
	}
	defer closeHandle(event)

	if err := ac.SetEventHandle(event); err != nil {
		log.Printf("capture: set event handle: %v", err)
		return
	}

	// Get the capture client interface.
	var acc *wca.IAudioCaptureClient
	if err := ac.GetService(wca.IID_IAudioCaptureClient, &acc); err != nil {
		log.Printf("capture: get capture client: %v", err)
		return
	}
	defer acc.Release()

	// Get buffer size for logging.
	var bufferFrames uint32
	if err := ac.GetBufferSize(&bufferFrames); err != nil {
		log.Printf("capture: get buffer size: %v", err)
		return
	}
	log.Printf("capture: buffer %d frames (%.1f ms)", bufferFrames,
		float64(bufferFrames)/float64(wfx.NSamplesPerSec)*1000)

	// Start the audio stream.
	if err := ac.Start(); err != nil {
		log.Printf("capture: start: %v", err)
		return
	}
	defer ac.Stop()

	log.Print("capture: started")

	channels := int(wfx.NChannels)
	bitsPerSample := int(wfx.WBitsPerSample)
	isFloat := wfx.WFormatTag == waveFormatIEEEFloat || isExtensibleFloat(wfx)

	// Validate device format against ring buffer expectations.
	if int(wfx.NSamplesPerSec) != c.ring.SampleRate() {
		log.Printf("capture: device sample rate %d Hz != ring buffer %d Hz, audio timing will be wrong",
			wfx.NSamplesPerSec, c.ring.SampleRate())
	}
	if channels != c.ring.Channels() {
		log.Printf("capture: device channels %d != ring buffer %d, audio layout will be wrong",
			channels, c.ring.Channels())
	}

	for {
		// Wait for either the buffer event or a stop signal.
		r := waitForSingleObject(event, 200) // 200ms timeout to check stop
		select {
		case <-c.stopCh:
			log.Print("capture: stopping")
			return
		default:
		}

		if r != 0 && r != 258 { // 258 = WAIT_TIMEOUT
			log.Printf("capture: wait error: %d", r)
			time.Sleep(10 * time.Millisecond)
			continue
		}

		// Drain all available packets.
		for {
			var packetSize uint32
			if err := acc.GetNextPacketSize(&packetSize); err != nil {
				log.Printf("capture: get packet size: %v", err)
				break
			}
			if packetSize == 0 {
				break
			}

			var data *byte
			var frames uint32
			var flags uint32
			if err := acc.GetBuffer(&data, &frames, &flags, nil, nil); err != nil {
				log.Printf("capture: get buffer: %v", err)
				break
			}

			if frames > 0 {
				if flags&wca.AUDCLNT_BUFFERFLAGS_SILENT != 0 {
					// Write zeros to keep ring buffer time-aligned with wall clock.
					c.ring.Write(make([]float32, int(frames)*channels))
				} else {
					samples := framesToFloat32(data, int(frames), channels, bitsPerSample, isFloat)
					c.ring.Write(samples)
				}
			}

			if err := acc.ReleaseBuffer(frames); err != nil {
				log.Printf("capture: release buffer: %v", err)
				break
			}
		}
	}
}

// framesToFloat32 converts raw WASAPI buffer data to interleaved float32 samples.
func framesToFloat32(data *byte, frames, channels, bitsPerSample int, isFloat bool) []float32 {
	totalSamples := frames * channels
	out := make([]float32, totalSamples)

	if isFloat && bitsPerSample == 32 {
		// Data is already float32 -- just copy.
		src := unsafe.Slice((*float32)(unsafe.Pointer(data)), totalSamples)
		copy(out, src)
		return out
	}

	// 16-bit PCM.
	if bitsPerSample == 16 {
		src := unsafe.Slice((*int16)(unsafe.Pointer(data)), totalSamples)
		for i, s := range src {
			out[i] = float32(s) / float32(math.MaxInt16)
		}
		return out
	}

	// 24-bit PCM (packed 3 bytes per sample).
	if bitsPerSample == 24 {
		raw := unsafe.Slice(data, frames*channels*3)
		for i := range totalSamples {
			offset := i * 3
			// Little-endian 24-bit signed.
			val := int32(raw[offset]) | int32(raw[offset+1])<<8 | int32(raw[offset+2])<<16
			if val&0x800000 != 0 {
				val |= ^0xFFFFFF // sign extend
			}
			out[i] = float32(val) / float32(1<<23)
		}
		return out
	}

	// Unknown format -- log and return silence rather than misinterpreting data.
	log.Printf("capture: unsupported audio format: %d-bit isFloat=%v, returning silence", bitsPerSample, isFloat)
	return out
}

// isExtensibleFloat checks whether a WAVE_FORMAT_EXTENSIBLE format uses float
// samples by reading the SubFormat GUID from the WAVEFORMATEXTENSIBLE structure.
func isExtensibleFloat(wfx *wca.WAVEFORMATEX) bool {
	if wfx.WFormatTag != 0xFFFE || wfx.CbSize < 22 {
		return false
	}
	// SubFormat GUID is at byte offset 24 from the start of WAVEFORMATEX:
	//   18 (sizeof WAVEFORMATEX) + 2 (Samples union) + 4 (dwChannelMask) = 24
	// KSDATAFORMAT_SUBTYPE_IEEE_FLOAT = {00000003-0000-0010-8000-00aa00389b71}
	subFormat := (*[16]byte)(unsafe.Add(unsafe.Pointer(wfx), 24))
	ieeeFloat := [16]byte{
		0x03, 0x00, 0x00, 0x00,
		0x00, 0x00,
		0x10, 0x00,
		0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71,
	}
	return *subFormat == ieeeFloat
}

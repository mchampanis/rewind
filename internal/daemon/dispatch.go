package daemon

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"rewind/internal/audio"
	"rewind/internal/config"
	"rewind/internal/dsp"
	"rewind/internal/ipc"
	"rewind/internal/vad"
)

// dispatch routes an incoming command to the correct handler.
func (d *Daemon) dispatch(cmd ipc.Command) ipc.Response {
	switch cmd.Name {

	case "status":
		return d.handleStatus()

	case "clip":
		return d.handleClip(cmd.Args)

	case "play":
		return d.handlePlay()

	case "stop":
		return d.handleStop()

	case "save":
		return d.handleSave(cmd.Args)

	case "yassify":
		return d.handleYassify(cmd.Args)

	case "device":
		return d.handleDevice(cmd.Args)

	case "quit":
		d.quit()
		return okResp(nil)

	default:
		return errResp(fmt.Errorf("unknown command: %s", cmd.Name))
	}
}

func (d *Daemon) handleStatus() ipc.Response {
	d.cfgMu.RLock()
	captureDevice := d.cfg.Device.Capture
	playbackDevice := d.cfg.Device.Playback
	capturer := d.capturer
	player := d.player
	d.cfgMu.RUnlock()

	data := map[string]any{
		"buffer_capacity_s":  d.ring.Capacity(),
		"buffer_available_s": d.ring.Available(),
		"sample_rate":        d.ring.SampleRate(),
		"channels":           d.ring.Channels(),
		"capture_device":     captureDevice,
		"playback_device":    playbackDevice,
		"capturing":          capturer.Running(),
		"playing":            player.Playing(),
	}

	if c := d.clip.Get(); c != nil {
		data["clip_duration_s"] = c.Duration()
	}

	return okResp(data)
}

func (d *Daemon) handleClip(args []string) ipc.Response {
	// clip [voice] [duration]
	voice := false
	durationStr := ""

	for _, arg := range args {
		switch {
		case arg == "voice":
			voice = true
		case durationStr == "":
			durationStr = arg
		default:
			return errResp(fmt.Errorf("clip: unexpected argument: %q", arg))
		}
	}

	seconds := 15.0 // default
	if durationStr != "" {
		n, err := strconv.ParseFloat(durationStr, 64)
		if err != nil || n <= 0 {
			return errResp(fmt.Errorf("clip: duration must be a positive number"))
		}
		seconds = n
	}

	samples, actual := d.ring.Snapshot(seconds)
	if len(samples) == 0 {
		return errResp(fmt.Errorf("buffer is empty"))
	}

	d.clip.Set(samples, d.ring.SampleRate(), d.ring.Channels())

	data := map[string]any{
		"duration_s": actual,
		"samples":    len(samples),
	}

	if voice {
		det, err := d.getDetector()
		if err != nil {
			return errResp(fmt.Errorf("VAD: %w", err))
		}
		segments, err := det.DetectSpeech(samples, d.ring.SampleRate(), d.ring.Channels(), 0.5)
		if err != nil {
			return errResp(fmt.Errorf("VAD: %w", err))
		}
		if len(segments) == 0 {
			return errResp(fmt.Errorf("no voice detected in clip"))
		}
		trimmed := vad.TrimToSegments(samples, d.ring.SampleRate(), d.ring.Channels(), segments)
		d.clip.Set(trimmed, d.ring.SampleRate(), d.ring.Channels())
		data["samples"] = len(trimmed)
		data["duration_s"] = float64(len(trimmed)) / float64(d.ring.SampleRate()) / float64(d.ring.Channels())
		data["voice_segments"] = len(segments)
	}

	return okResp(data)
}

func (d *Daemon) handlePlay() ipc.Response {
	c := d.clip.Get()
	if c == nil {
		return errResp(fmt.Errorf("no active clip (use 'clip' first)"))
	}

	d.cfgMu.RLock()
	player := d.player
	d.cfgMu.RUnlock()

	if err := player.Play(c.Samples(), c.SampleRate(), c.Channels()); err != nil {
		return errResp(err)
	}

	return okResp(map[string]any{
		"duration_s": c.Duration(),
		"playing":    true,
	})
}

func (d *Daemon) handleStop() ipc.Response {
	d.cfgMu.RLock()
	player := d.player
	d.cfgMu.RUnlock()

	if !player.Playing() {
		return errResp(fmt.Errorf("nothing is playing"))
	}
	player.Stop()
	return okResp(nil)
}

func (d *Daemon) handleSave(args []string) ipc.Response {
	c := d.clip.Get()
	if c == nil {
		return errResp(fmt.Errorf("no active clip (use 'clip' first)"))
	}

	path := argAt(args, 0)
	if path == "" {
		// Default: save to %APPDATA%\rewind\clips\<timestamp>.wav
		clipsDir := filepath.Join(config.Dir(), "clips")
		if err := os.MkdirAll(clipsDir, 0755); err != nil {
			return errResp(fmt.Errorf("create clips dir: %w", err))
		}
		path = filepath.Join(clipsDir, fmt.Sprintf("clip_%s.wav", time.Now().Format("20060102_150405.000")))
	}

	if err := c.SaveWAV(path); err != nil {
		return errResp(err)
	}

	return okResp(map[string]any{"path": path})
}

func (d *Daemon) handleYassify(args []string) ipc.Response {
	if len(args) == 0 {
		return errResp(fmt.Errorf("yassify: specify one or more effect names or presets"))
	}

	c := d.clip.Get()
	if c == nil {
		return errResp(fmt.Errorf("no active clip (use 'clip' first)"))
	}

	// Resolve presets and effects into a flat effect chain.
	d.cfgMu.RLock()
	var chain []string
	for _, name := range args {
		if preset, ok := d.cfg.Yassify.Presets[name]; ok {
			chain = append(chain, preset.Effects...)
		} else {
			chain = append(chain, name)
		}
	}
	// Copy effects map so Apply works on owned data outside the lock.
	effects := make(map[string]config.EffectConfig, len(d.cfg.Yassify.Effects))
	for k, v := range d.cfg.Yassify.Effects {
		effects[k] = v
	}
	d.cfgMu.RUnlock()

	samples, err := dsp.Apply(c.Samples(), c.SampleRate(), c.Channels(), chain, effects)
	if err != nil {
		return errResp(fmt.Errorf("yassify: %w", err))
	}

	c.SetSamples(samples)

	return okResp(map[string]any{
		"effects":    chain,
		"duration_s": c.Duration(),
	})
}

func (d *Daemon) handleDevice(args []string) ipc.Response {
	sub := argAt(args, 0)
	switch sub {
	case "list":
		devices, err := audio.ListDevices()
		if err != nil {
			return errResp(err)
		}
		list := make([]map[string]any, len(devices))
		for i, dev := range devices {
			list[i] = map[string]any{"name": dev.Name, "flow": dev.Flow}
		}
		return okResp(map[string]any{"devices": list})

	case "capture":
		name := argAt(args, 1)
		if name == "" {
			d.cfgMu.RLock()
			v := d.cfg.Device.Capture
			d.cfgMu.RUnlock()
			return okResp(map[string]any{"capture": v})
		}
		d.cfgMu.Lock()
		old := d.cfg.Device.Capture
		d.cfg.Device.Capture = name
		err := config.Save(d.cfg)
		if err != nil {
			d.cfg.Device.Capture = old
			d.cfgMu.Unlock()
			return errResp(err)
		}
		// Restart capture with the new device. If it fails, roll back
		// the config and keep the old capturer running.
		prev := d.capturer
		prev.Stop()
		next := audio.NewCapturer(d.ring, name)
		if err := next.Start(); err != nil {
			d.cfg.Device.Capture = old
			_ = config.Save(d.cfg)
			d.capturer = audio.NewCapturer(d.ring, old)
			if startErr := d.capturer.Start(); startErr != nil {
				log.Printf("device: failed to restore capture on %q: %v", old, startErr)
			}
			d.cfgMu.Unlock()
			return errResp(fmt.Errorf("capture restart failed: %w", err))
		}
		d.capturer = next
		d.cfgMu.Unlock()
		return okResp(map[string]any{"capture": name})

	case "playback":
		name := argAt(args, 1)
		if name == "" {
			d.cfgMu.RLock()
			v := d.cfg.Device.Playback
			d.cfgMu.RUnlock()
			return okResp(map[string]any{"playback": v})
		}
		d.cfgMu.Lock()
		old := d.cfg.Device.Playback
		d.cfg.Device.Playback = name
		err := config.Save(d.cfg)
		if err != nil {
			d.cfg.Device.Playback = old
			d.cfgMu.Unlock()
			return errResp(err)
		}
		// Replace the player so the next play command targets the new device.
		// Stop any in-progress playback first.
		d.player.Stop()
		d.player = audio.NewPlayer(name)
		d.cfgMu.Unlock()
		return okResp(map[string]any{"playback": name})

	case "":
		d.cfgMu.RLock()
		capture := d.cfg.Device.Capture
		playback := d.cfg.Device.Playback
		d.cfgMu.RUnlock()
		return okResp(map[string]any{
			"capture":  capture,
			"playback": playback,
		})

	default:
		return errResp(fmt.Errorf("device: expected list|capture|playback"))
	}
}

// helpers

func okResp(data map[string]any) ipc.Response {
	return ipc.Response{OK: true, Data: data}
}

func errResp(err error) ipc.Response {
	return ipc.Response{OK: false, Error: err.Error()}
}

func argAt(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}

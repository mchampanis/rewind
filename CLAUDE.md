# rewind

Rolling audio buffer daemon for Windows. Captures audio from a configurable output device (e.g. Voicemeeter virtual output) into a RAM ring buffer, then provides commands to clip, analyse (VAD), play back, save, and apply effects ("yassify").

## Architecture

```
cmd/rewind/         Entry point (daemon mode or IPC client)
internal/
  audio/             WASAPI loopback capture (capture.go) and render playback (render.go)
  buffer/            Circular float32 ring buffer (RAM only)
  clip/              Active clip management (snapshot from ring buffer)
  config/            TOML configuration (%APPDATA%\rewind\config.toml)
  daemon/            Lifecycle, systray, IPC dispatch
  dsp/               Built-in DSP effects (echo, reverb, bandpass, gain, pitch) [not yet implemented]
  icon/              Embedded tray icon
  ipc/               Named pipe server/client, JSON protocol
  vad/               Silero VAD via ONNX Runtime (model embedded via go:embed)
  yassify/           Effect chain engine (built-in + Lua plugins) [not yet implemented]
```

## Key design decisions

- Ring buffer is entirely in RAM (no disk writes) to preserve SSD and minimize latency
- Audio capture via WASAPI loopback on the configured output device
- Playback via WASAPI render to the configured input device
- Voice detection uses Silero VAD (ONNX Runtime) for accuracy in noisy environments
- Effects system has three tiers: built-in DSP (TOML params), Lua scripts, effect chain presets
- 48kHz float32 stereo throughout

## Build

Requires MinGW (CGO for systray) and `onnxruntime.dll` in PATH or beside the binary.

```
make            # debug build (console visible, race detector)
make release    # release build (no console, stripped)
make install    # release + copy to %LOCALAPPDATA%\rewind\
```

## IPC

Named pipe: `\\.\pipe\rewind`
Protocol: JSON lines, same as wincon. See `internal/ipc/`.

## Dependencies

- `go-wca` for WASAPI (capture + playback)
- `onnxruntime-go` for Silero VAD inference
- `gopher-lua` for Lua effect plugins
- `fyne.io/systray` for tray icon
- `go-winio` for named pipes
- `BurntSushi/toml` for config

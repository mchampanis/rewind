# rewind

Rolling audio buffer daemon for Windows. Captures audio from a configurable output device into RAM, then lets you clip, analyse, play back, save, and yassify audio on demand.

## Install

```
make install
```

Requires `onnxruntime.dll` beside the binary (bundled with releases).

## Usage

Start the daemon:

```
rewind daemon
```

Send commands:

```
rewind status                        # buffer health, device info
rewind clip 15                       # snapshot last 15 seconds
rewind clip voice 30                 # VAD-crop voice from last 30 seconds
rewind play                          # play active clip to virtual input
rewind stop                          # stop playback
rewind save                          # save clip as WAV
rewind save C:\clips\funny.wav       # save to specific path
rewind yassify airhorn               # apply airhorn preset
rewind yassify echo airhorn          # chain multiple effects
rewind device list                   # list audio endpoints
rewind device capture <name>         # set capture device
rewind device playback <name>        # set playback device
rewind quit                          # stop daemon
```

## Configuration

`%APPDATA%\rewind\config.toml` - created with defaults on first run.

## Effects

Three tiers:

1. **Built-in DSP** - configured in TOML (echo, reverb, bandpass/lowpass/highpass, gain, pitch shift, speed, scratch, WAV sample overlay)
2. **Lua scripts** - drop `.lua` files into `%APPDATA%\rewind\effects\` for auto-discovery at startup
3. **Presets** - named chains of built-in and Lua effects in TOML (e.g. "airhorn", "radio", "remix")

## License

MIT

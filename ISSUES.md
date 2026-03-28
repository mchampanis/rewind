# Issues

## Open

### #4 - DSP effects engine
Implement built-in DSP effects: echo, reverb, bandpass filter, gain, pitch shift, speed. Parameterized via TOML config.

### #5 - Lua plugin system
Integrate gopher-lua for scriptable effects. Expose audio/dsp/samples API to Lua scripts. Auto-discover .lua files in %APPDATA%\rewind\effects\.

## Closed

### #1 - WASAPI loopback capture
Implemented in internal/audio/capture.go.

### #6 - Device enumeration
Implemented in internal/audio/device.go, wired up via `device list` command.

### #7 - Placeholder icon
Placeholder .ico embedded in internal/icon/.

### #8 - COM lifetime leaks in device lookup
findDevice/getDefaultRenderDevice called comInit redundantly when caller already owned COM, leaking a COM reference. ListDevices lacked OS thread lock for COM safety. Fixed: removed redundant comInit from internal helpers, added runtime.LockOSThread to ListDevices.

### #9 - WAVE_FORMAT_EXTENSIBLE float detection
capture.go assumed EXTENSIBLE with 32-bit was always float. Now checks the SubFormat GUID to distinguish float from int32 PCM.

### #10 - SaveWAV ignored write errors and was slow
clip.SaveWAV used per-sample streaming writes with no error checking. Rewrote to build WAV in memory and write atomically via os.WriteFile.

### #11 - refreshTray goroutine never exited
Tray status refresh loop had no stop signal. Now selects on a done channel closed during teardown.

### #12 - Stub commands returned fake success
handlePlay and handleStop returned success despite being unimplemented. Now return explicit "not yet implemented" errors.

### #13 - Autostart requested admin elevation
schtasks /RL HIGHEST triggered UAC on login. Changed to /RL LIMITED.

### #14 - No IPC read deadline
Named pipe connections had no timeout. Added 10s read deadline, reset after each message.

### #15 - Config race between IPC handlers
Daemon.cfg read/written from multiple goroutines without synchronization. Added cfgMu RWMutex to protect config access.

### #16 - cap builtin shadowed in ring.go
Ring.Write and Ring.Snapshot used `cap` as a local variable name. Renamed to `size`.

### #3 - Silero VAD integration
Implemented in internal/vad/. Silero VAD v5 model embedded via go:embed, inference via onnxruntime-go. Downsamples 48kHz stereo to 16kHz mono, processes 512-sample frames, groups voiced frames into segments with padding and merging. Wired into `clip voice` command for auto-trimming.

### #2 - WASAPI render playback
Implemented in internal/audio/render.go. Player struct mirrors Capturer lifecycle: COM on locked OS thread, event-driven buffer feeding, stop/done channels. Dispatch wired for play/stop commands. Capture device change now restarts capture automatically.

package dsp

import (
	"fmt"
	"path/filepath"
	"strings"

	"rewind/internal/config"

	lua "github.com/yuin/gopher-lua"
)

// RunLuaEffect executes a Lua script as an audio effect. A fresh sandboxed VM
// is created for each call. The script receives globals (samples, sample_rate,
// channels, cfg) and a dsp module with built-in effects. It should return a
// table of float32 samples; returning nothing passes through the input.
func RunLuaEffect(samples []float32, sampleRate, channels int, scriptPath string, cfg config.EffectConfig) ([]float32, error) {
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer L.Close()

	// Sandbox: only safe standard libraries.
	for _, pair := range []struct {
		name string
		fn   lua.LGFunction
	}{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
	} {
		L.Push(L.NewFunction(pair.fn))
		L.Push(lua.LString(pair.name))
		L.Call(1, 0)
	}

	// Set audio globals.
	L.SetGlobal("samples", sliceToTable(L, samples))
	L.SetGlobal("sample_rate", lua.LNumber(sampleRate))
	L.SetGlobal("channels", lua.LNumber(channels))
	L.SetGlobal("cfg", cfgToTable(L, cfg))

	// Register the dsp module.
	registerDSPModule(L)

	if err := L.DoFile(scriptPath); err != nil {
		return nil, fmt.Errorf("lua script %q: %w", filepath.Base(scriptPath), err)
	}

	// Check if the script returned a table.
	ret := L.Get(-1)
	if tbl, ok := ret.(*lua.LTable); ok {
		return tableToSlice(tbl), nil
	}

	// No return value: pass through input unchanged.
	return samples, nil
}

// DiscoverLuaEffects scans dir for .lua files and returns an EffectConfig
// entry for each, keyed by filename stem.
func DiscoverLuaEffects(dir string) map[string]config.EffectConfig {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.lua"))
	if len(matches) == 0 {
		return nil
	}
	effects := make(map[string]config.EffectConfig, len(matches))
	for _, path := range matches {
		base := filepath.Base(path)
		name := strings.TrimSuffix(base, ".lua")
		effects[name] = config.EffectConfig{
			Type: "lua",
			Path: base,
		}
	}
	return effects
}

// sliceToTable converts a float32 slice to a 1-indexed Lua table of numbers.
func sliceToTable(L *lua.LState, samples []float32) *lua.LTable {
	tbl := L.CreateTable(len(samples), 0)
	for _, s := range samples {
		tbl.Append(lua.LNumber(s))
	}
	return tbl
}

// tableToSlice converts a 1-indexed Lua table of numbers to a float32 slice.
func tableToSlice(tbl *lua.LTable) []float32 {
	n := tbl.Len()
	out := make([]float32, n)
	for i := 1; i <= n; i++ {
		if v, ok := tbl.RawGetInt(i).(lua.LNumber); ok {
			out[i-1] = float32(v)
		}
	}
	return out
}

// cfgToTable builds a Lua table from the EffectConfig fields so scripts can
// read parameters set in TOML (e.g. cfg.gain, cfg.delay_ms).
func cfgToTable(L *lua.LState, cfg config.EffectConfig) *lua.LTable {
	tbl := L.NewTable()
	L.SetField(tbl, "type", lua.LString(cfg.Type))
	L.SetField(tbl, "path", lua.LString(cfg.Path))
	L.SetField(tbl, "placement", lua.LString(cfg.Placement))
	L.SetField(tbl, "filter", lua.LString(cfg.Filter))
	L.SetField(tbl, "low_hz", lua.LNumber(cfg.LowHz))
	L.SetField(tbl, "high_hz", lua.LNumber(cfg.HighHz))
	L.SetField(tbl, "delay_ms", lua.LNumber(cfg.DelayMs))
	L.SetField(tbl, "decay", lua.LNumber(cfg.Decay))
	L.SetField(tbl, "gain", lua.LNumber(cfg.Gain))
	L.SetField(tbl, "shift", lua.LNumber(cfg.Shift))
	L.SetField(tbl, "speed", lua.LNumber(cfg.Speed))
	return tbl
}

// registerDSPModule registers the "dsp" module exposing built-in effect
// functions to Lua scripts.
func registerDSPModule(L *lua.LState) {
	mod := L.NewTable()

	mod.RawSetString("gain", L.NewFunction(luaGain))
	mod.RawSetString("echo", L.NewFunction(luaEcho))
	mod.RawSetString("reverb", L.NewFunction(luaReverb))
	mod.RawSetString("filter", L.NewFunction(luaFilter))
	mod.RawSetString("pitch", L.NewFunction(luaPitch))
	mod.RawSetString("speed", L.NewFunction(luaSpeed))
	mod.RawSetString("clamp", L.NewFunction(luaClamp))

	L.SetGlobal("dsp", mod)
}

// checkSamplesArg extracts a float32 slice from the first Lua argument.
func checkSamplesArg(L *lua.LState) []float32 {
	tbl := L.CheckTable(1)
	return tableToSlice(tbl)
}

// luaGain: dsp.gain(samples, factor) -> table
func luaGain(L *lua.LState) int {
	s := checkSamplesArg(L)
	factor := L.CheckNumber(2)
	out := Gain(s, float64(factor))
	L.Push(sliceToTable(L, out))
	return 1
}

// luaEcho: dsp.echo(samples, sample_rate, channels, delay_ms, decay) -> table
func luaEcho(L *lua.LState) int {
	s := checkSamplesArg(L)
	sr := L.CheckInt(2)
	ch := L.CheckInt(3)
	delayMs := L.CheckInt(4)
	decay := L.CheckNumber(5)
	out := Echo(s, sr, ch, delayMs, float64(decay))
	L.Push(sliceToTable(L, out))
	return 1
}

// luaReverb: dsp.reverb(samples, sample_rate, channels, delay_ms, decay) -> table
func luaReverb(L *lua.LState) int {
	s := checkSamplesArg(L)
	sr := L.CheckInt(2)
	ch := L.CheckInt(3)
	delayMs := L.CheckInt(4)
	decay := L.CheckNumber(5)
	out := Reverb(s, sr, ch, delayMs, float64(decay))
	L.Push(sliceToTable(L, out))
	return 1
}

// luaFilter: dsp.filter(samples, sample_rate, channels, filter_type, low_hz, high_hz) -> table
func luaFilter(L *lua.LState) int {
	s := checkSamplesArg(L)
	sr := L.CheckInt(2)
	ch := L.CheckInt(3)
	filterType := L.CheckString(4)
	lowHz := L.CheckInt(5)
	highHz := L.CheckInt(6)
	out := Filter(s, sr, ch, filterType, lowHz, highHz)
	L.Push(sliceToTable(L, out))
	return 1
}

// luaPitch: dsp.pitch(samples, channels, semitones) -> table
func luaPitch(L *lua.LState) int {
	s := checkSamplesArg(L)
	ch := L.CheckInt(2)
	semitones := L.CheckNumber(3)
	out := Pitch(s, ch, float64(semitones))
	L.Push(sliceToTable(L, out))
	return 1
}

// luaSpeed: dsp.speed(samples, channels, factor) -> table
func luaSpeed(L *lua.LState) int {
	s := checkSamplesArg(L)
	ch := L.CheckInt(2)
	factor := L.CheckNumber(3)
	out := Speed(s, ch, float64(factor))
	L.Push(sliceToTable(L, out))
	return 1
}

// luaClamp: dsp.clamp(samples) -> table
func luaClamp(L *lua.LState) int {
	s := checkSamplesArg(L)
	out := make([]float32, len(s))
	copy(out, s)
	clamp(out)
	L.Push(sliceToTable(L, out))
	return 1
}

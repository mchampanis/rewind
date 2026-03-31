package dsp

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"rewind/internal/config"
)

func writeTempLua(t *testing.T, dir, name, script string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunLuaEffectPassthrough(t *testing.T) {
	dir := t.TempDir()
	path := writeTempLua(t, dir, "pass.lua", `-- do nothing, no return`)

	in := []float32{0.5, -0.5, 0.25, -0.25}
	out, err := RunLuaEffect(in, 48000, 2, path, config.EffectConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Fatalf("len: got %d, want %d", len(out), len(in))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("sample %d: got %f, want %f", i, out[i], in[i])
		}
	}
}

func TestRunLuaEffectCustomLogic(t *testing.T) {
	dir := t.TempDir()
	path := writeTempLua(t, dir, "half.lua", `
		local out = {}
		for i = 1, #samples do
			out[i] = samples[i] * 0.5
		end
		return out
	`)

	in := []float32{1.0, -1.0, 0.4, -0.4}
	out, err := RunLuaEffect(in, 48000, 2, path, config.EffectConfig{})
	if err != nil {
		t.Fatal(err)
	}
	want := []float32{0.5, -0.5, 0.2, -0.2}
	for i := range want {
		if math.Abs(float64(out[i]-want[i])) > 1e-6 {
			t.Errorf("sample %d: got %f, want %f", i, out[i], want[i])
		}
	}
}

func TestRunLuaEffectDSPGain(t *testing.T) {
	dir := t.TempDir()
	path := writeTempLua(t, dir, "gain.lua", `
		return dsp.gain(samples, 2.0)
	`)

	in := []float32{0.3, -0.3}
	out, err := RunLuaEffect(in, 48000, 2, path, config.EffectConfig{})
	if err != nil {
		t.Fatal(err)
	}
	want := []float32{0.6, -0.6}
	for i := range want {
		if math.Abs(float64(out[i]-want[i])) > 1e-6 {
			t.Errorf("sample %d: got %f, want %f", i, out[i], want[i])
		}
	}
}

func TestRunLuaEffectDSPEcho(t *testing.T) {
	dir := t.TempDir()
	path := writeTempLua(t, dir, "echo.lua", `
		return dsp.echo(samples, sample_rate, channels, 100, 0.5)
	`)

	// 48000 Hz stereo: 100ms = 4800 samples per channel = 9600 interleaved.
	// Echo adds a tail, so output should be longer than input.
	in := make([]float32, 9600)
	in[0] = 1.0 // impulse in left channel
	in[1] = 1.0 // impulse in right channel
	out, err := RunLuaEffect(in, 48000, 2, path, config.EffectConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) <= len(in) {
		t.Errorf("echo should extend output: got %d, input was %d", len(out), len(in))
	}
}

func TestRunLuaEffectCfgAccess(t *testing.T) {
	dir := t.TempDir()
	path := writeTempLua(t, dir, "cfggain.lua", `
		return dsp.gain(samples, cfg.gain)
	`)

	in := []float32{0.4, -0.4}
	cfg := config.EffectConfig{Gain: 0.5}
	out, err := RunLuaEffect(in, 48000, 2, path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []float32{0.2, -0.2}
	for i := range want {
		if math.Abs(float64(out[i]-want[i])) > 1e-6 {
			t.Errorf("sample %d: got %f, want %f", i, out[i], want[i])
		}
	}
}

func TestRunLuaEffectScriptError(t *testing.T) {
	dir := t.TempDir()
	path := writeTempLua(t, dir, "bad.lua", `this is not valid lua!!!`)

	_, err := RunLuaEffect([]float32{0.1}, 48000, 1, path, config.EffectConfig{})
	if err == nil {
		t.Fatal("expected error from invalid Lua script")
	}
}

func TestRunLuaEffectSandbox(t *testing.T) {
	dir := t.TempDir()
	path := writeTempLua(t, dir, "escape.lua", `
		os.execute("echo pwned")
		return samples
	`)

	_, err := RunLuaEffect([]float32{0.1}, 48000, 1, path, config.EffectConfig{})
	if err == nil {
		t.Fatal("expected error: os should not be available in sandbox")
	}
}

func TestDiscoverLuaEffects(t *testing.T) {
	dir := t.TempDir()
	writeTempLua(t, dir, "alpha.lua", "return samples")
	writeTempLua(t, dir, "beta.lua", "return samples")
	// Non-lua file should be ignored.
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hi"), 0644)

	effects := DiscoverLuaEffects(dir)
	if len(effects) != 2 {
		t.Fatalf("expected 2 effects, got %d", len(effects))
	}
	for _, name := range []string{"alpha", "beta"} {
		ec, ok := effects[name]
		if !ok {
			t.Errorf("missing effect %q", name)
			continue
		}
		if ec.Type != "lua" {
			t.Errorf("effect %q: type = %q, want lua", name, ec.Type)
		}
		if ec.Path != name+".lua" {
			t.Errorf("effect %q: path = %q, want %q", name, ec.Path, name+".lua")
		}
	}
}

func TestDiscoverLuaEffectsEmptyDir(t *testing.T) {
	dir := t.TempDir()
	effects := DiscoverLuaEffects(dir)
	if effects != nil {
		t.Errorf("expected nil for empty dir, got %v", effects)
	}
}

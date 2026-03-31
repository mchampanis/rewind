// Package daemon implements the rewind background process, tray icon, and IPC server.
package daemon

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"fyne.io/systray"
	"golang.org/x/sys/windows"

	"rewind/internal/audio"
	"rewind/internal/buffer"
	"rewind/internal/clip"
	"rewind/internal/config"
	"rewind/internal/dsp"
	"rewind/internal/icon"
	"rewind/internal/ipc"
	"rewind/internal/vad"
)

var (
	modUser32      = windows.NewLazySystemDLL("user32.dll")
	procMessageBox = modUser32.NewProc("MessageBoxW")
)

// fatal logs a message and shows a MessageBox before exiting.
// Necessary because windowsgui builds have no console for stderr.
func fatal(msg string) {
	log.Print("FATAL: " + msg)
	title, _ := windows.UTF16PtrFromString("rewind -- fatal error")
	text, _ := windows.UTF16PtrFromString(msg)
	procMessageBox.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
	os.Exit(1)
}

// Daemon is the running rewind background process.
type Daemon struct {
	cfg   *config.Config
	cfgMu sync.RWMutex
	ring  *buffer.Ring
	clip  *clip.Store

	capturer *audio.Capturer
	player   *audio.Player
	detector *vad.Detector // lazily initialized on first use
	ipcLn    net.Listener
	done     chan struct{} // closed on teardown to signal goroutines

	// tray menu items updated at runtime
	menuStatus    *systray.MenuItem
	menuAutostart *systray.MenuItem
}

// Run starts the daemon. Blocks until quit is called.
// Must be called from the main goroutine (systray requirement).
func Run() {
	if err := setupLog(); err != nil {
		log.Printf("warning: could not set up log file: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		fatal(fmt.Sprintf("load config: %v", err))
	}
	log.Printf("config loaded from %s", config.Path())

	// Create effects and samples directories.
	if err := os.MkdirAll(config.EffectsDir(), 0755); err != nil {
		log.Printf("warning: create effects dir: %v", err)
	}
	if err := os.MkdirAll(config.SamplesDir(), 0755); err != nil {
		log.Printf("warning: create samples dir: %v", err)
	}

	// Auto-discover Lua effect scripts.
	luaEffects := dsp.DiscoverLuaEffects(config.EffectsDir())
	for name, ec := range luaEffects {
		if _, exists := cfg.Yassify.Effects[name]; !exists {
			cfg.Yassify.Effects[name] = ec
		}
	}
	if n := len(luaEffects); n > 0 {
		log.Printf("discovered %d Lua effect(s)", n)
	}

	if err := vad.InitRuntime(); err != nil {
		log.Printf("warning: ONNX runtime init failed (VAD unavailable): %v", err)
	}

	ring := buffer.New(cfg.Buffer.Duration, cfg.Buffer.SampleRate, cfg.Buffer.Channels)
	log.Printf("ring buffer: %ds @ %dHz %dch (%.1f MB)",
		cfg.Buffer.Duration, cfg.Buffer.SampleRate, cfg.Buffer.Channels,
		float64(cfg.Buffer.Duration*cfg.Buffer.SampleRate*cfg.Buffer.Channels*4)/(1024*1024))

	ipcLn, err := ipc.Listen()
	if err != nil {
		fatal(fmt.Sprintf("IPC listen: %v", err))
	}

	capturer := audio.NewCapturer(ring, cfg.Device.Capture)
	player := audio.NewPlayer(cfg.Device.Playback)

	d := &Daemon{
		cfg:      cfg,
		ring:     ring,
		clip:     clip.NewStore(),
		capturer: capturer,
		player:   player,
		ipcLn:    ipcLn,
		done:     make(chan struct{}),
	}

	systray.Run(d.setup, d.teardown)
}

func (d *Daemon) setup() {
	d.buildTray()

	// Start capture before accepting IPC commands so a device-change
	// request cannot race with the initial Start() call.
	if err := d.capturer.Start(); err != nil {
		log.Printf("warning: capture failed to start: %v", err)
	}

	go d.serveIPC()
	go d.refreshTray()

	log.Print("daemon started")
}

func (d *Daemon) teardown() {
	close(d.done)
	d.ipcLn.Close()
	d.cfgMu.RLock()
	player := d.player
	capturer := d.capturer
	d.cfgMu.RUnlock()
	player.Stop()
	capturer.Stop()
	if d.detector != nil {
		d.detector.Close()
	}
	vad.DestroyRuntime()
	if logFile != nil {
		log.SetOutput(os.Stderr)
		logFile.Close()
	}
}

func (d *Daemon) serveIPC() {
	if err := ipc.Serve(d.ipcLn, d.dispatch); err != nil {
		// Expected when listener is closed during teardown.
		select {
		case <-d.done:
		default:
			log.Printf("IPC server stopped: %v", err)
		}
	}
}

func (d *Daemon) quit() {
	systray.Quit()
}

func (d *Daemon) buildTray() {
	systray.SetIcon(icon.Bytes())
	systray.SetTooltip("rewind")

	d.menuStatus = systray.AddMenuItem(
		fmt.Sprintf("Buffer: %.0fs / %.0fs", d.ring.Available(), d.ring.Capacity()),
		"Ring buffer status",
	)
	d.menuStatus.Disable()

	systray.AddSeparator()

	d.menuAutostart = systray.AddMenuItemCheckbox(
		"Start on login",
		"Register rewind to start automatically on login",
		d.cfg.Daemon.Autostart,
	)

	systray.AddSeparator()

	menuLogs := systray.AddMenuItem("Open log file", "Open the rewind log")
	menuQuit := systray.AddMenuItem("Quit rewind", "Stop the daemon")

	go func() {
		for {
			select {
			case <-d.menuAutostart.ClickedCh:
				d.toggleAutostart()
			case <-menuLogs.ClickedCh:
				openLog()
			case <-menuQuit.ClickedCh:
				d.quit()
			case <-d.done:
				return
			}
		}
	}()
}

// refreshTray periodically updates the buffer status in the tray menu.
func (d *Daemon) refreshTray() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			avail := d.ring.Available()
			capacity := d.ring.Capacity()
			d.cfgMu.RLock()
			capturer := d.capturer
			d.cfgMu.RUnlock()
			capturing := ""
			if capturer.Running() {
				capturing = " [capturing]"
			}
			d.menuStatus.SetTitle(fmt.Sprintf("Buffer: %.0fs / %.0fs%s", avail, capacity, capturing))
		case <-d.done:
			return
		}
	}
}

func (d *Daemon) toggleAutostart() {
	d.cfgMu.Lock()
	want := !d.cfg.Daemon.Autostart

	if want {
		if err := registerAutostart(); err != nil {
			d.cfgMu.Unlock()
			log.Printf("register autostart: %v", err)
			return
		}
	} else {
		if err := unregisterAutostart(); err != nil {
			d.cfgMu.Unlock()
			log.Printf("unregister autostart: %v", err)
			return
		}
	}

	d.cfg.Daemon.Autostart = want
	err := config.Save(d.cfg)
	if err != nil {
		d.cfg.Daemon.Autostart = !want
	}
	d.cfgMu.Unlock()

	if err != nil {
		log.Printf("save config: %v", err)
		return
	}

	if want {
		d.menuAutostart.Check()
	} else {
		d.menuAutostart.Uncheck()
	}
}

// getDetector lazily initializes the VAD detector on first use.
func (d *Daemon) getDetector() (*vad.Detector, error) {
	d.cfgMu.Lock()
	defer d.cfgMu.Unlock()
	if d.detector != nil {
		return d.detector, nil
	}
	det, err := vad.NewDetector()
	if err != nil {
		return nil, err
	}
	d.detector = det
	return det, nil
}

func openLog() {
	logPath := filepath.Join(config.Dir(), "rewind.log")
	if err := exec.Command("explorer.exe", logPath).Start(); err != nil {
		log.Printf("open log: %v", err)
	}
}

var logFile *os.File

func setupLog() error {
	if err := os.MkdirAll(config.Dir(), 0755); err != nil {
		return err
	}
	logPath := filepath.Join(config.Dir(), "rewind.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	logFile = f
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	return nil
}

func registerAutostart() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	cmd := exec.Command("schtasks", "/Create",
		"/TN", "rewind",
		"/TR", fmt.Sprintf(`"%s" daemon`, exe),
		"/SC", "ONLOGON",
		"/RL", "LIMITED",
		"/F",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks: %w -- %s", err, out)
	}
	return nil
}

func unregisterAutostart() error {
	cmd := exec.Command("schtasks", "/Delete", "/TN", "rewind", "/F")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks delete: %w -- %s", err, out)
	}
	return nil
}

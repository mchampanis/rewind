package main

import (
	"fmt"
	"os"

	"rewind/internal/daemon"
	"rewind/internal/ipc"
)

// version is set at build time via -ldflags "-X main.version=<tag>".
var version = "dev"

var usage = fmt.Sprintf(`rewind %s -- rolling audio buffer daemon`, version) + `

Usage:
  rewind daemon            Start the background daemon (run once at login)
  rewind <command> [args]  Send a command to the running daemon

Commands:
  status                    Buffer health, seconds captured, device info

  clip [duration]           Snapshot last N seconds (default 15)
  clip voice [duration]     VAD-crop voice from last N seconds

  play                      Play active clip to playback device
  stop                      Stop playback

  save [path]               Save active clip as WAV

  yassify <preset> [...]    Apply effect chain to active clip

  device list               List audio endpoints
  device capture <name>     Set capture device (WASAPI loopback source)
  device playback <name>    Set playback device (WASAPI render target)

  quit                      Stop the running daemon
`

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		fmt.Print(usage)
		os.Exit(0)
	}

	if args[0] == "daemon" {
		if ipc.IsRunning() {
			fmt.Fprintln(os.Stderr, "rewind daemon is already running")
			os.Exit(1)
		}
		daemon.Run() // blocks
		return
	}

	// forward command to the running daemon
	resp, err := ipc.Send(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if !resp.OK {
		fmt.Fprintln(os.Stderr, "error:", resp.Error)
		os.Exit(1)
	}

	if len(resp.Data) > 0 {
		fmt.Println(resp.String())
	}
}

package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

const PipeName = `\\.\pipe\rewind`

// Handler processes a command and returns a response.
type Handler func(Command) Response

// Listen creates the named pipe listener.
// The caller owns the listener and must close it to stop accepting connections.
func Listen() (net.Listener, error) {
	// D:P(A;;GA;;;OW) -- protected DACL; grant generic-all to the pipe owner only.
	ln, err := winio.ListenPipe(PipeName, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;OW)",
	})
	if err != nil {
		return nil, fmt.Errorf("listen pipe: %w", err)
	}
	return ln, nil
}

// Serve accepts connections on the listener and dispatches commands to the handler.
// Blocks until the listener is closed.
func Serve(ln net.Listener, handler Handler) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go handleConn(conn, handler)
	}
}

func handleConn(conn net.Conn, handler Handler) {
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		cmd, err := DecodeCommand(scanner.Bytes())
		if err != nil {
			writeResponse(conn, Response{OK: false, Error: "invalid command: " + err.Error()})
			conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			continue
		}
		resp := handler(cmd)
		writeResponse(conn, resp)
		// Reset deadline after response, so slow handlers don't eat into the next read's budget.
		conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	}
	if err := scanner.Err(); err != nil {
		log.Printf("ipc: connection error: %v", err)
	}
}

func writeResponse(conn net.Conn, r Response) {
	b, err := r.Encode()
	if err != nil {
		log.Printf("encode response: %v", err)
		return
	}
	if _, err := conn.Write(b); err != nil {
		log.Printf("write response: %v", err)
	}
}

// Send connects to the daemon pipe, sends a command, and returns the response.
func Send(args []string) (Response, error) {
	if len(args) == 0 {
		return Response{}, fmt.Errorf("no command")
	}

	timeout := 2 * time.Second
	conn, err := winio.DialPipe(PipeName, &timeout)
	if err != nil {
		return Response{}, fmt.Errorf("daemon not running (start with: rewind daemon): %w", err)
	}
	defer conn.Close()

	cmd := Command{Name: args[0], Args: args[1:]}
	b, err := json.Marshal(cmd)
	if err != nil {
		return Response{}, err
	}
	b = append(b, '\n')

	if _, err := conn.Write(b); err != nil {
		return Response{}, err
	}

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	scanner := bufio.NewScanner(conn)
	if scanner.Scan() {
		return DecodeResponse(scanner.Bytes())
	}
	if err := scanner.Err(); err != nil {
		return Response{}, err
	}
	return Response{}, fmt.Errorf("no response from daemon")
}

// IsRunning returns true if a daemon is already listening on the pipe.
func IsRunning() bool {
	timeout := 200 * time.Millisecond
	conn, err := winio.DialPipe(PipeName, &timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

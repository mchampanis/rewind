package audio

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// createEvent creates a Windows auto-reset event object.
func createEvent() (uintptr, error) {
	h, err := windows.CreateEvent(nil, 0, 0, nil) // auto-reset, initially non-signaled
	if err != nil {
		return 0, fmt.Errorf("CreateEvent: %w", err)
	}
	return uintptr(h), nil
}

const waitTimeout = 258 // WAIT_TIMEOUT

// waitForSingleObject waits on a handle for the given timeout in milliseconds.
// Returns 0 (WAIT_OBJECT_0) on signal, waitTimeout on timeout.
func waitForSingleObject(handle uintptr, timeoutMs uint32) uint32 {
	r, _ := windows.WaitForSingleObject(windows.Handle(handle), timeoutMs)
	return r
}

// closeHandle closes a Windows handle.
func closeHandle(handle uintptr) {
	windows.CloseHandle(windows.Handle(handle))
}

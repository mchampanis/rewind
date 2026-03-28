// Package icon provides the embedded tray icon for rewind.
// TODO: Replace placeholder with actual icon.
package icon

import _ "embed"

//go:embed rewind.ico
var iconBytes []byte

// Bytes returns the raw ICO data for the system tray icon.
func Bytes() []byte {
	return iconBytes
}

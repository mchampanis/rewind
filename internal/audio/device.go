// Package audio implements WASAPI loopback capture and render playback.
package audio

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

// Device describes an audio endpoint.
type Device struct {
	Name string `json:"name"`
	Flow string `json:"flow"` // "output" or "input"
}

// comInit initializes COM on the current thread. Safe to call multiple times.
func comInit() error {
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		if oleErr, ok := err.(*ole.OleError); ok && oleErr.Code() == 1 {
			return nil // S_FALSE: already initialized
		}
		return fmt.Errorf("CoInitializeEx: %w", err)
	}
	return nil
}

// ListDevices enumerates all active audio endpoints.
func ListDevices() ([]Device, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := comInit(); err != nil {
		return nil, err
	}
	defer ole.CoUninitialize()

	var de *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(
		wca.CLSID_MMDeviceEnumerator, 0,
		wca.CLSCTX_ALL, wca.IID_IMMDeviceEnumerator, &de,
	); err != nil {
		return nil, fmt.Errorf("create device enumerator: %w", err)
	}
	defer de.Release()

	var devices []Device

	for _, flow := range []struct {
		flag uint32
		name string
	}{
		{wca.ERender, "output"},
		{wca.ECapture, "input"},
	} {
		var dc *wca.IMMDeviceCollection
		if err := de.EnumAudioEndpoints(flow.flag, wca.DEVICE_STATE_ACTIVE, &dc); err != nil {
			continue
		}

		var count uint32
		dc.GetCount(&count)
		for i := range count {
			var dev *wca.IMMDevice
			if err := dc.Item(i, &dev); err != nil {
				continue
			}
			name, err := deviceFriendlyName(dev)
			dev.Release()
			if err != nil {
				continue
			}
			devices = append(devices, Device{Name: name, Flow: flow.name})
		}
		dc.Release()
	}

	return devices, nil
}

// findDevice looks up an active endpoint by substring match on friendly name.
// flow should be wca.ERender or wca.ECapture.
// Caller must have COM initialized on the current OS thread.
func findDevice(name string, flow uint32) (*wca.IMMDevice, error) {
	var de *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(
		wca.CLSID_MMDeviceEnumerator, 0,
		wca.CLSCTX_ALL, wca.IID_IMMDeviceEnumerator, &de,
	); err != nil {
		return nil, fmt.Errorf("create device enumerator: %w", err)
	}
	defer de.Release()

	var dc *wca.IMMDeviceCollection
	if err := de.EnumAudioEndpoints(flow, wca.DEVICE_STATE_ACTIVE, &dc); err != nil {
		return nil, fmt.Errorf("enum endpoints: %w", err)
	}
	defer dc.Release()

	nameLower := strings.ToLower(name)
	var count uint32
	dc.GetCount(&count)
	for i := range count {
		var dev *wca.IMMDevice
		if err := dc.Item(i, &dev); err != nil {
			continue
		}
		fname, err := deviceFriendlyName(dev)
		if err != nil {
			dev.Release()
			continue
		}
		if strings.Contains(strings.ToLower(fname), nameLower) {
			return dev, nil
		}
		dev.Release()
	}

	return nil, fmt.Errorf("device not found: %q", name)
}

// getDefaultRenderDevice returns the default render endpoint.
// Caller must have COM initialized on the current OS thread.
func getDefaultRenderDevice() (*wca.IMMDevice, error) {
	var de *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(
		wca.CLSID_MMDeviceEnumerator, 0,
		wca.CLSCTX_ALL, wca.IID_IMMDeviceEnumerator, &de,
	); err != nil {
		return nil, fmt.Errorf("create device enumerator: %w", err)
	}
	defer de.Release()

	var dev *wca.IMMDevice
	if err := de.GetDefaultAudioEndpoint(wca.ERender, wca.EConsole, &dev); err != nil {
		return nil, fmt.Errorf("get default render endpoint: %w", err)
	}
	return dev, nil
}

func deviceFriendlyName(dev *wca.IMMDevice) (string, error) {
	var ps *wca.IPropertyStore
	if err := dev.OpenPropertyStore(wca.STGM_READ, &ps); err != nil {
		return "", fmt.Errorf("open property store: %w", err)
	}
	defer ps.Release()

	var pv wca.PROPVARIANT
	if err := ps.GetValue(&wca.PKEY_Device_FriendlyName, &pv); err != nil {
		return "", fmt.Errorf("get friendly name: %w", err)
	}
	return pv.String(), nil
}

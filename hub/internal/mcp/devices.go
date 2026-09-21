package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/adefesioso/homedash/hub/internal/store"
)

// deviceTools are the Network tab as tools: what the remotes can see,
// and your word on it. Nothing here touches a device; that is a job or
// a command on the remote that sees it.
func deviceTools(srv *sdk.Server, st *store.Store) {
	addTool(srv, &sdk.Tool{
		Name:        "list_devices",
		Description: "Every device the remotes can see that is not itself enrolled: LAN neighbours (kind lan, addr is the MAC; sightings carry ip, iface, hostname, mDNS services, SSDP targets), Wi-Fi networks in range (kind wifi, addr is the BSSID; ssid, signal) and Bluetooth devices (kind bt). Each has the hub's guess (guessName, guessKind, vendor) and the user's word (name, userKind), which wins. Sightings say which remote last saw it and when — to act on a device, give a job or run a command on a remote that sees it. The hub never reaches a device itself. Also lists which remotes can scan LAN, Wi-Fi and Bluetooth.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, devicesOut, error) {
		ds, err := st.Devices(ctx)
		if err != nil {
			return nil, devicesOut{}, err
		}
		sc, err := st.HostScans(ctx)
		if err != nil {
			return nil, devicesOut{}, err
		}
		return nil, devicesOut{Devices: ds, Scanners: sc}, nil
	})

	addTool(srv, &sdk.Tool{
		Name:        "name_device",
		Description: "Record what a device is: a name and a kind (tv, speaker, printer, phone, computer, camera, light, appliance, router, console, input, network, or free text), beside the hub's guess. Empty clears. Use it when you have worked out what something is, the way the user would name it in the panel.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: ptr(false)},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in nameDeviceIn) (*sdk.CallToolResult, okOut, error) {
		d, err := st.Device(ctx, in.Device)
		if err != nil {
			return nil, okOut{}, err
		}
		if err := st.NameDevice(ctx, d.ID, in.Name, in.Kind); err != nil {
			return nil, okOut{}, err
		}
		return nil, okOut{OK: true}, nil
	})
}

type devicesOut struct {
	Devices  []store.Device   `json:"devices"`
	Scanners []store.HostScan `json:"scanners"`
}
type nameDeviceIn struct {
	Device string `json:"device" jsonschema:"the device's id, or kind:addr such as lan:a4:83:e7:00:11:22"`
	Name   string `json:"name,omitempty" jsonschema:"what to call it; empty clears"`
	Kind   string `json:"kind,omitempty" jsonschema:"what it is; empty clears"`
}

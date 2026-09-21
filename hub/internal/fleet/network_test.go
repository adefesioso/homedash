package fleet

import (
	"encoding/json"
	"testing"

	"github.com/adefesioso/homedash/hub/internal/store"
)

func sighting(detail string) store.Sighting {
	return store.Sighting{Detail: json.RawMessage(detail)}
}

func TestIdentify(t *testing.T) {
	cases := []struct {
		name                    string
		dev                     store.Device
		wantName, wantKind, ven string
	}{
		{"chromecast by service", store.Device{Kind: "lan", Addr: "f4:f5:d8:00:00:01", Sightings: []store.Sighting{
			sighting(`{"hostname":"living.local","mdnsName":"Living\\032Room\\032TV","services":["_googlecast._tcp"]}`)}},
			"Living Room TV", "tv", "Google"},
		{"printer over ssdp beats vendor", store.Device{Kind: "lan", Addr: "00:00:00:00:00:01", Sightings: []store.Sighting{
			sighting(`{"hostname":"","ssdp":["urn:schemas-upnp-org:device:Printer:1"]}`)}},
			"", "printer", ""},
		{"router by ssdp", store.Device{Kind: "lan", Addr: "44:9b:c1:00:00:01", Sightings: []store.Sighting{
			sighting(`{"hostname":"_gateway","ssdp":["urn:schemas-upnp-org:device:InternetGatewayDevice:1"]}`)}},
			"_gateway", "router", ""},
		{"vendor alone", store.Device{Kind: "lan", Addr: "5c:aa:fd:00:00:01", Sightings: []store.Sighting{sighting(`{}`)}},
			"", "speaker", "Sonos"},
		{"private mac is a phone", store.Device{Kind: "lan", Addr: "b2:75:2a:b6:b5:87", Sightings: []store.Sighting{sighting(`{}`)}},
			"private address", "phone", ""},
		{"bluetooth headset", store.Device{Kind: "bt", Addr: "6c:47:60:b3:08:45", Sightings: []store.Sighting{
			sighting(`{"name":"JBL WAVE100TWS","icon":"audio-headset"}`)}},
			"JBL WAVE100TWS", "speaker", ""},
		{"bluetooth name that is the address", store.Device{Kind: "bt", Addr: "12:34:66:78:dd:4b", Sightings: []store.Sighting{
			sighting(`{"name":"12-34-66-78-DD-4B","icon":""}`)}},
			"", "", ""},
		{"wifi network", store.Device{Kind: "wifi", Addr: "6c:44:2a:3b:88:a0", Sightings: []store.Sighting{
			sighting(`{"ssid":"CASA TOMADA","signal":100}`)}},
			"CASA TOMADA", "network", ""},
	}
	for _, c := range cases {
		name, kind, vendor := identify(&c.dev)
		if name != c.wantName || kind != c.wantKind || vendor != c.ven {
			t.Errorf("%s: got (%q, %q, %q), want (%q, %q, %q)", c.name, name, kind, vendor, c.wantName, c.wantKind, c.ven)
		}
	}
}

func TestHostMACs(t *testing.T) {
	got := hostMACs(json.RawMessage(`{"hostname":"x","interfaces":[{"name":"eno1","mac":"BC:FC:E7:B3:79:8E"}]}`))
	if len(got) != 1 || got[0] != "bc:fc:e7:b3:79:8e" {
		t.Fatalf("got %v", got)
	}
}

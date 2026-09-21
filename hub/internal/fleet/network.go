package fleet

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/adefesioso/homedash/hub/internal/store"
)

//go:embed scan.sh
var scanScript string

// Scan is the parsed shape of what scan.sh prints. A nil slice means the
// machine has no interface or tool for it.
type Scan struct {
	LAN []struct {
		MAC        string   `json:"mac"`
		IP         string   `json:"ip"`
		Iface      string   `json:"iface"`
		Hostname   string   `json:"hostname"`
		MDNSName   string   `json:"mdnsName"`
		SSDPServer string   `json:"ssdpServer"`
		Services   []string `json:"services"`
		SSDP       []string `json:"ssdp"`
	} `json:"lan"`
	WiFi []struct {
		BSSID    string   `json:"bssid"`
		SSID     string   `json:"ssid"`
		Signal   *float64 `json:"signal"`
		Freq     *float64 `json:"freq"`
		Security string   `json:"security"`
	} `json:"wifi"`
	BT []struct {
		MAC  string   `json:"mac"`
		Name string   `json:"name"`
		Icon string   `json:"icon"`
		RSSI *float64 `json:"rssi"`
	} `json:"bt"`
}

// ScanLoop asks every online host what it can see, every
// network.scan_minutes (10; 0 switches scanning off), and forgets what
// nobody has seen for network.forget_days (30).
func (f *Fleet) ScanLoop(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	var last time.Time
	for {
		every := f.settingInt(ctx, "network.scan_minutes", 10)
		if every > 0 && time.Since(last) >= time.Duration(every)*time.Minute {
			f.ScanAll(ctx)
			last = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// ScanAll is one scan of every online host, then the forgetting.
func (f *Fleet) ScanAll(ctx context.Context) {
	hosts, err := f.Store.Hosts(ctx)
	if err != nil {
		return
	}
	each(ctx, hosts, func(h *store.Host) {
		if h.Status == "online" {
			f.ScanHost(ctx, h)
		}
	})
	if ctx.Err() != nil {
		return
	}
	if err := f.Store.ForgetStale(ctx, f.settingInt(ctx, "network.forget_days", 30)); err != nil {
		f.Log.Error("forget stale devices", "err", err)
	}
}

// ScanHost pipes scan.sh to one host and merges what it saw.
func (f *Fleet) ScanHost(ctx context.Context, h *store.Host) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	r, err := f.Exec.Sh(ctx, Target(h), scanScript)
	if err != nil {
		_ = f.Store.SetHostScan(ctx, h.ID, false, false, false, err.Error())
		return
	}
	if r.ExitCode != 0 || !json.Valid(bytes.TrimSpace(r.Stdout)) {
		f.Log.Warn("scan script failed", "host", h.Name, "exit", r.ExitCode, "stderr", tail(r.Stderr))
		_ = f.Store.SetHostScan(ctx, h.ID, false, false, false, "scan script failed: "+tail(r.Stderr))
		return
	}
	var sc Scan
	if err := json.Unmarshal(r.Stdout, &sc); err != nil {
		_ = f.Store.SetHostScan(ctx, h.ID, false, false, false, "unreadable scan: "+err.Error())
		return
	}
	if err := f.Merge(ctx, h, &sc); err != nil {
		f.Log.Error("merge scan", "host", h.Name, "err", err)
	}
	_ = f.Store.SetHostScan(ctx, h.ID, sc.LAN != nil, sc.WiFi != nil, sc.BT != nil, "")
}

// Merge records one host's sightings, skipping the hub's own hosts by
// the interfaces their facts report, and recomputes each seen device's
// guess from every current sighting.
func (f *Fleet) Merge(ctx context.Context, h *store.Host, sc *Scan) error {
	hosts, err := f.Store.Hosts(ctx)
	if err != nil {
		return err
	}
	own := map[string]bool{}
	for i := range hosts {
		for _, m := range hostMACs(hosts[i].Facts) {
			own[m] = true
		}
	}
	// The hub itself is never a host record (it can't enroll itself), so
	// its own interfaces have to be excluded a different way: read them
	// directly, the same as hostMACs reads a remote's (N-x).
	for _, m := range localMACs() {
		own[m] = true
	}
	hubAddr, _ := f.Store.Setting(ctx, "hub.lan_addr")
	hubAddr = strings.TrimSpace(hubAddr)
	// own also has to erase what an earlier scan, before this host or the
	// hub itself was excluded, already recorded — otherwise a stale row
	// simply stops being updated and never goes away on its own (N-x).
	ownAddrs := make([]string, 0, len(own))
	for m := range own {
		ownAddrs = append(ownAddrs, m)
	}
	if err := f.Store.ForgetAddrs(ctx, ownAddrs); err != nil {
		return err
	}
	// Everything this scan saw, then one transaction for all of it.
	var found []store.Seen
	sight := func(kind, addr string, detail any) {
		addr = strings.ToLower(addr)
		if addr == "" || own[addr] {
			return
		}
		b, _ := json.Marshal(detail)
		found = append(found, store.Seen{Kind: kind, Addr: addr, Detail: b})
	}
	for _, d := range sc.LAN {
		if hubAddr != "" && d.IP == hubAddr {
			continue
		}
		sight("lan", d.MAC, d)
	}
	for _, w := range sc.WiFi {
		sight("wifi", w.BSSID, w)
	}
	for _, b := range sc.BT {
		sight("bt", b.MAC, b)
	}
	seen, created, err := f.Store.SightAll(ctx, h.ID, found)
	if err != nil {
		return err
	}
	for i, d := range found {
		if created[i] {
			_ = f.Store.RecordEvent(ctx, "device.new", d.Kind+":"+d.Addr, h.Name+" saw a new "+kindLabel(d.Kind)+" "+d.Addr)
		}
	}
	devs, err := f.Store.Devices(ctx)
	if err != nil {
		return err
	}
	byID := map[int64]*store.Device{}
	for i := range devs {
		byID[devs[i].ID] = &devs[i]
	}
	for _, id := range seen {
		d := byID[id]
		if d == nil {
			continue
		}
		name, kind, vendor := identify(d)
		if name != d.GuessName || kind != d.GuessKind || vendor != d.Vendor {
			if err := f.Store.SetGuess(ctx, id, name, kind, vendor); err != nil {
				return err
			}
		}
	}
	return nil
}

func kindLabel(kind string) string {
	return map[string]string{"lan": "device", "wifi": "Wi-Fi network", "bt": "Bluetooth device"}[kind]
}

// localMACs reads the hub process's own interfaces — the hub runs on one
// of these machines too, and a remote's scan sees it by its real
// hardware address, the same as any other neighbour (N-x).
func localMACs() []string {
	out := []string{}
	ifs, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, i := range ifs {
		if mac := strings.ToLower(i.HardwareAddr.String()); mac != "" {
			out = append(out, mac)
		}
	}
	return out
}

// hostMACs reads the interfaces a host's facts report.
func hostMACs(facts json.RawMessage) []string {
	var p struct {
		Interfaces []struct {
			MAC string `json:"mac"`
		} `json:"interfaces"`
	}
	_ = json.Unmarshal(facts, &p)
	out := []string{}
	for _, i := range p.Interfaces {
		out = append(out, strings.ToLower(i.MAC))
	}
	return out
}

// mdnsServiceKinds maps what a device advertises to what it probably is,
// checked in order so the more specific service wins.
var mdnsServiceKinds = []struct{ svc, kind string }{
	{"_ipp.", "printer"}, {"_ipps.", "printer"}, {"_printer.", "printer"}, {"_pdl-datastream.", "printer"}, {"_scanner.", "printer"},
	{"_axis-video.", "camera"}, {"_rtsp.", "camera"},
	{"_hue.", "light"},
	{"_raop.", "speaker"}, {"_sonos.", "speaker"}, {"_spotify-connect.", "speaker"},
	{"_googlecast.", "tv"}, {"_airplay.", "tv"}, {"_amzn-wplay.", "tv"}, {"_androidtvremote", "tv"}, {"_viziocast.", "tv"},
	{"_hap.", "appliance"}, {"_matter.", "appliance"}, {"_homekit.", "appliance"},
	{"_companion-link.", "phone"}, {"_rdlink.", "phone"},
	{"_ssh.", "computer"}, {"_sftp-ssh.", "computer"}, {"_smb.", "computer"}, {"_afpovertcp.", "computer"}, {"_workstation.", "computer"}, {"_nfs.", "computer"},
}

var avahiEscape = regexp.MustCompile(`\\(\d{3})`)

// unescapeAvahi turns avahi's parseable `\032` into the character.
func unescapeAvahi(s string) string {
	return avahiEscape.ReplaceAllStringFunc(s, func(m string) string {
		n, _ := strconv.Atoi(m[1:])
		return string(rune(n))
	})
}

// identify is the hub's guess from a device's current sightings: a
// name, a kind and a vendor, each empty when nothing says.
func identify(d *store.Device) (name, kind, vendor string) {
	vendor, vkind, private := vendorOf(d.Addr)
	switch d.Kind {
	case "wifi":
		for _, s := range d.Sightings {
			var w struct {
				SSID string `json:"ssid"`
			}
			if json.Unmarshal(s.Detail, &w) == nil && w.SSID != "" {
				name = w.SSID
				break
			}
		}
		return name, "network", vendor
	case "bt":
		icons := map[string]string{
			"phone": "phone", "computer": "computer", "video-display": "tv", "camera-video": "camera", "camera-photo": "camera",
			"printer": "printer", "audio-card": "speaker", "audio-headset": "speaker", "audio-headphones": "speaker",
			"input-keyboard": "input", "input-mouse": "input", "input-gaming": "input", "input-tablet": "input",
		}
		for _, s := range d.Sightings {
			var b struct {
				Name string `json:"name"`
				Icon string `json:"icon"`
			}
			if json.Unmarshal(s.Detail, &b) != nil {
				continue
			}
			if name == "" && b.Name != "" && !strings.EqualFold(strings.ReplaceAll(b.Name, "-", ":"), d.Addr) {
				name = b.Name
			}
			if kind == "" {
				kind = icons[b.Icon]
			}
		}
		if kind == "" {
			kind = vkind
		}
		return name, kind, vendor
	}
	var hostname string
	for _, s := range d.Sightings {
		var l struct {
			Hostname   string   `json:"hostname"`
			MDNSName   string   `json:"mdnsName"`
			SSDPServer string   `json:"ssdpServer"`
			Services   []string `json:"services"`
			SSDP       []string `json:"ssdp"`
		}
		if json.Unmarshal(s.Detail, &l) != nil {
			continue
		}
		if name == "" && l.MDNSName != "" {
			name = unescapeAvahi(l.MDNSName)
		}
		if hostname == "" && l.Hostname != "" {
			hostname = strings.TrimSuffix(strings.TrimSuffix(l.Hostname, ".local"), ".lan")
		}
		for _, svc := range l.Services {
			for _, m := range mdnsServiceKinds {
				if kind == "" && strings.HasPrefix(svc, m.svc) {
					kind = m.kind
				}
			}
		}
		for _, st := range l.SSDP {
			st = strings.ToLower(st)
			switch {
			case kind != "":
			case strings.Contains(st, "mediarenderer"), strings.Contains(st, "dial-multiscreen"), strings.Contains(st, "roku:ecp"):
				kind = "tv"
			case strings.Contains(st, "internetgatewaydevice"), strings.Contains(st, "wanconnectiondevice"):
				kind = "router"
			case strings.Contains(st, "printer"):
				kind = "printer"
			}
		}
		if kind == "" && l.SSDPServer != "" {
			srv := strings.ToLower(l.SSDPServer)
			if strings.Contains(srv, "roku") || strings.Contains(srv, "bravia") || strings.Contains(srv, "webos") || strings.Contains(srv, "tizen") {
				kind = "tv"
			}
		}
	}
	if name == "" {
		name = hostname
	}
	if kind == "" {
		kind = vkind
	}
	if private && name == "" {
		name = "private address"
		if kind == "" {
			kind = "phone"
		}
	}
	return name, kind, vendor
}

// settingInt reads a numeric setting with a default for empty or bad.
func (f *Fleet) settingInt(ctx context.Context, key string, def int) int {
	v, _ := f.Store.Setting(ctx, key)
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
		return n
	}
	return def
}

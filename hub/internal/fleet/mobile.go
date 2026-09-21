package fleet

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/adefesioso/homedash/hub/internal/store"
)

// mobileStale is how long a phone can go without a status push before the
// heartbeat calls it offline. The app is expected to push every minute or
// so while it is reachable; three misses is a real gap, not a hiccup.
const mobileStale = 3 * time.Minute

// NewMobileCode mints a pairing code for an Android remote: not a script
// to curl, since a phone has no root shell to pipe it into, but a code
// the companion app posts back to the enrollment listener along with the
// phone's own report — the same door a compute remote's `enroll.sh`
// knocks on, answered differently.
func (f *Fleet) NewMobileCode(ctx context.Context, name string) (*store.EnrollCode, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "", errors.New("a remote needs a name")
	}
	code := randomCode()
	c, err := f.Store.NewEnrollCode(ctx, code, name, "mobile", 0)
	if err != nil {
		return nil, "", err
	}
	return c, fmt.Sprintf("%s/enroll/%s", f.EnrollURL(), code), nil
}

// mobileReport is what the companion app posts to spend a pairing code:
// the phone's own facts, in the shape MobileFacts reads.
type mobileReport struct {
	Model          string          `json:"model"`
	AndroidVersion string          `json:"androidVersion"`
	Facts          json.RawMessage `json:"facts"`
}

// ReportMobile is the mobile half of Report: a pairing code spent by the
// app instead of a machine's enrollment script. There is no host key to
// pin — a phone is not dialed into, it calls in — so the hub mints a
// device key here and hands it back once; every status push after this
// must carry it.
func (f *Fleet) ReportMobile(ctx context.Context, code string, body []byte) (h *store.Host, deviceKey string, err error) {
	c, err := f.Store.EnrollCode(ctx, code)
	if err != nil {
		return nil, "", err
	}
	if c == nil {
		return nil, "", errors.New("code is not live")
	}
	var in mobileReport
	if len(body) > 0 {
		if err := json.Unmarshal(body, &in); err != nil {
			return nil, "", fmt.Errorf("report: %w", err)
		}
	}
	if !json.Valid(in.Facts) || len(in.Facts) == 0 {
		in.Facts = json.RawMessage("{}")
	}
	facts, err := mergeMobileFacts(in.Facts, in.Model, in.AndroidVersion)
	if err != nil {
		return nil, "", err
	}
	ok, err := f.Store.UseEnrollCode(ctx, code)
	if err != nil || !ok {
		return nil, "", errors.New("code already used")
	}
	deviceKey = randomToken()
	host := &store.Host{Name: c.Name, Kind: "mobile", HostKey: deviceKey, Facts: facts}
	id, err := f.Store.AddHost(ctx, host)
	if err != nil {
		return nil, "", err
	}
	host.ID = id
	if _, err := f.Store.SetHostStatus(ctx, id, "online", facts, ""); err != nil {
		return nil, "", err
	}
	f.Notify("host.enrolled", host.Name, fmt.Sprintf("%s (Android) paired", host.Name))
	return host, deviceKey, nil
}

// mergeMobileFacts folds the model and Android version the app reports in
// beside its own facts blob, the way compute's facts.sh reports hostname
// alongside everything else, so the card has one object to read.
func mergeMobileFacts(facts json.RawMessage, model, androidVersion string) (json.RawMessage, error) {
	var m map[string]any
	if err := json.Unmarshal(facts, &m); err != nil || m == nil {
		m = map[string]any{}
	}
	if model != "" {
		m["model"] = model
	}
	if androidVersion != "" {
		m["androidVersion"] = androidVersion
	}
	return json.Marshal(m)
}

// SetMobileStatus is one status push from the companion app: the device
// key it was handed at pairing, and whatever it can currently read off
// the phone. Wrong key or a non-mobile host is refused; there is no SSH
// connection to distrust here, only this shared secret.
func (f *Fleet) SetMobileStatus(ctx context.Context, h *store.Host, deviceKey string, facts json.RawMessage) error {
	if h.Kind != "mobile" {
		return errors.New("not a mobile remote")
	}
	if deviceKey == "" || subtle.ConstantTimeCompare([]byte(deviceKey), []byte(h.HostKey)) != 1 {
		return errors.New("wrong device key")
	}
	if !json.Valid(facts) || len(facts) == 0 {
		facts = json.RawMessage("{}")
	}
	var model, androidVersion string
	var raw struct {
		Model          string `json:"model"`
		AndroidVersion string `json:"androidVersion"`
	}
	_ = json.Unmarshal(h.Facts, &raw)
	model, androidVersion = raw.Model, raw.AndroidVersion
	merged, err := mergeMobileFacts(facts, model, androidVersion)
	if err != nil {
		return err
	}
	_, err = f.Store.SetHostStatus(ctx, h.ID, "online", merged, "")
	return err
}

// markStaleMobile calls a phone offline once it has gone quiet longer
// than mobileStale: nothing dials a phone the way Sweep dials an SSH
// host, so absence has to be noticed from the outside, by the clock
// alone.
func (f *Fleet) markStaleMobile(ctx context.Context, hosts []store.Host) {
	now := time.Now().UTC()
	for i := range hosts {
		h := &hosts[i]
		if h.Kind != "mobile" || h.Status == "offline" || h.LastSeen == "" {
			continue
		}
		seen, err := time.Parse(time.RFC3339, h.LastSeen)
		if err != nil {
			continue
		}
		if now.Sub(seen) < mobileStale {
			continue
		}
		if _, err := f.Store.SetHostStatus(ctx, h.ID, "offline", nil, ""); err == nil {
			f.Notify("host.offline", h.Name, h.Name+" went offline")
		}
	}
}

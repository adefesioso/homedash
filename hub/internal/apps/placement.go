package apps

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/adefesioso/homedash/hub/internal/fleet"
	"github.com/adefesioso/homedash/hub/internal/store"
)

// Verdict is one host's line in a placement: fits or not, a score, and
// the sentence that says why.
type Verdict struct {
	Host   string  `json:"host"`
	HostID int64   `json:"hostId"`
	Fits   bool    `json:"fits"`
	Score  float64 `json:"score"`
	Why    string  `json:"why"`
}

// Gateway is what placement asks about a cluster: which host its path
// appears on. Nil until storage clusters exist.
type Gateway func(ctx context.Context, cluster string) (*store.Host, error)

// Place ranks the fleet for a stack's needs. It is arithmetic, not a
// model — no key, no inference — so it gives the same answer twice. A
// candidate has to leave margin, not just fit exactly, and the disk
// trend counts: a machine free right now but filling for a month does
// not win a placement it can't hold.
func (a *Apps) Place(ctx context.Context, needs Needs, gateway Gateway) ([]Verdict, error) {
	hosts, err := a.Store.Hosts(ctx)
	if err != nil {
		return nil, err
	}
	var only *store.Host
	if needs.Cluster != "" && gateway != nil {
		only, err = gateway(ctx, needs.Cluster)
		if err != nil {
			return nil, err
		}
	}
	out := []Verdict{}
	for i := range hosts {
		h := &hosts[i]
		v := Verdict{Host: h.Name, HostID: h.ID}
		if only != nil && h.ID != only.ID {
			v.Why = "the stack wants cluster " + needs.Cluster + ", whose path is on " + only.Name
			out = append(out, v)
			continue
		}
		if h.Status != "online" {
			v.Why = "is " + h.Status
			out = append(out, v)
			continue
		}
		var f fleet.Facts
		_ = json.Unmarshal(h.Facts, &f)
		var rootFree int64
		for _, m := range f.Mounts {
			if m.Path == "/" {
				rootFree = m.Free
			}
		}
		memAvail := f.MemTotal - f.MemUsed
		needMem := int64(needs.MemoryMB) << 20
		needDisk := int64(needs.DiskGB) << 30
		trend, window := a.trend(ctx, h.ID) // bytes per day, negative when filling; window is days of history behind it
		switch {
		case f.Docker == nil:
			v.Why = "has no Docker"
		case needs.GPU && f.GPU == nil:
			v.Why = "has no GPU"
		case f.Cores < needs.Cores:
			v.Why = fmt.Sprintf("has %d cores, the stack wants %d", f.Cores, needs.Cores)
		case memAvail < needMem*3/2:
			v.Why = fmt.Sprintf("has %d MB free, which leaves no margin over the %d MB the stack wants", memAvail>>20, needs.MemoryMB)
		case rootFree < needDisk*6/5:
			v.Why = fmt.Sprintf("has %d GB free, which leaves no margin over the %d GB the stack wants", rootFree>>30, needs.DiskGB)
		case trend < 0 && needDisk > 0 && fillDays(rootFree-needDisk, trend, window) < 30:
			v.Why = fillSentence(rootFree-needDisk, trend, window)
		default:
			v.Fits = true
			// Score: headroom, weighted so memory and disk matter most,
			// with a penalty for a filling disk and a small one for load.
			memScore := float64(memAvail-needMem) / float64(f.MemTotal+1)
			diskScore := float64(rootFree-needDisk) / float64(rootFree+needDisk+1)
			coreScore := float64(f.Cores-needs.Cores) / float64(f.Cores+1)
			loadPenalty := f.Load1 / float64(f.Cores+1)
			trendPenalty := 0.0
			if trend < 0 {
				trendPenalty = 0.2
			}
			v.Score = 0.4*memScore + 0.35*diskScore + 0.15*coreScore - 0.1*loadPenalty - trendPenalty
			v.Why = fmt.Sprintf("leaves %d MB of memory and %d GB of disk after the stack", (memAvail-needMem)>>20, (rootFree-needDisk)>>30)
			if trend < 0 {
				v.Why += fmt.Sprintf(", though its disk is filling at %d MB a day", int64(-trend)>>20)
			}
			if f.GPU != nil && needs.GPU {
				v.Why += ", and has the GPU"
			}
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Fits != out[j].Fits {
			return out[i].Fits
		}
		return out[i].Score > out[j].Score
	})
	return out, nil
}

// trend is the root mount's free-space slope over the last week, in
// bytes per day, from the metrics the heartbeat kept, and the number of
// days of history behind it. Zero, zero when there is not enough history
// to say.
func (a *Apps) trend(ctx context.Context, hostID int64) (bytesPerDay, windowDays float64) {
	ms, err := a.Store.HostMetrics(ctx, hostID, 24*7)
	if err != nil || len(ms) < 12 {
		return 0, 0
	}
	var xs, ys []float64
	for _, m := range ms {
		var mounts map[string]float64
		if json.Unmarshal(m.Mounts, &mounts) != nil {
			continue
		}
		t, err := time.Parse(time.RFC3339, m.At)
		if err != nil {
			continue
		}
		if free, ok := mounts["/"]; ok {
			xs = append(xs, float64(t.Unix())/86400)
			ys = append(ys, free)
		}
	}
	if len(xs) < 12 || xs[len(xs)-1]-xs[0] < 0.5 {
		return 0, 0
	}
	window := xs[len(xs)-1] - xs[0]
	ys = dropBurst(ys)
	// Least squares slope.
	var sx, sy, sxx, sxy float64
	n := float64(len(xs))
	for i := range xs {
		sx += xs[i]
		sy += ys[i]
		sxx += xs[i] * xs[i]
		sxy += xs[i] * ys[i]
	}
	den := n*sxx - sx*sx
	if den == 0 {
		return 0, 0
	}
	return (n*sxy - sx*sy) / den, window
}

// dropBurst removes one sample-to-sample jump from the free-space
// series before it feeds the trend line: a model pull, a big delete —
// one step, not a slope — when it alone accounts for more than half the
// week's net change (A-2b: "the fill-rate estimate was skewed by one 400
// MB model pull earlier in the day"). Every sample from the jump onward
// is shifted back by it, so a real trend on top of a one-time burst
// still comes through undamped. At most one jump is ever removed: a
// standing trend is made of many small steps, a burst is one big one.
func dropBurst(ys []float64) []float64 {
	total := ys[len(ys)-1] - ys[0]
	if total == 0 {
		return ys
	}
	worst, jump := -1, 0.0
	for i := 1; i < len(ys); i++ {
		if d := ys[i] - ys[i-1]; math.Abs(d) > math.Abs(jump) {
			worst, jump = i, d
		}
	}
	if worst < 0 || math.Abs(jump) < 0.5*math.Abs(total) {
		return ys
	}
	out := append([]float64(nil), ys...)
	for i := worst; i < len(out); i++ {
		out[i] -= jump
	}
	return out
}

// fillDays is how long rootFree, minus what the stack wants, holds out
// against a negative trend — capped at the window of history the trend
// came from, since a slope from a week of samples earns no claim about
// the month after it, and floored so it's never negative.
func fillDays(rootFree int64, trendBytesPerDay, windowDays float64) float64 {
	days := float64(rootFree) / -trendBytesPerDay
	if days < 0 {
		days = 0
	}
	if windowDays > 0 && days > windowDays {
		days = windowDays
	}
	return days
}

// fillSentence phrases fillDays without ever saying "0 days" — a
// disk that's already past the line reads as full today, not as a
// countdown that hit zero (A-2b).
func fillSentence(rootFree int64, trendBytesPerDay, windowDays float64) string {
	rate := int64(-trendBytesPerDay) >> 20
	days := fillDays(rootFree, trendBytesPerDay, windowDays)
	if days < 1 {
		return fmt.Sprintf("is filling at %d MB a day and would be full today with the stack on it", rate)
	}
	return fmt.Sprintf("is filling at %d MB a day and would be full in about %d days with the stack on it", rate, int(days))
}

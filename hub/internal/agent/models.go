package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Provider is one provider the vault can run, with its model ids as omp
// names them (the id is the half after "provider/" in a setting).
type Provider struct {
	Name   string   `json:"name"`
	Models []string `json:"models"`
}

// modelsTTL is how long a listing is reused: a picker opened twice in a
// minute does not run omp twice.
const modelsTTL = time.Minute

var (
	modelsMu   sync.Mutex
	modelsAt   time.Time
	modelsList []Provider
)

// Models is what the hub's omp can run: `omp models --json` with the
// hub's environment, so credentials resolve through the vault and only
// providers holding one (plus homedash, the pool) come back. Grouped by
// provider, sorted, cached for modelsTTL. A window opening or a picker
// rendering both read this same cache rather than each paying for their
// own discovery.
func (a *Agent) Models(ctx context.Context) ([]Provider, error) {
	return a.models(ctx, false)
}

// RefreshModels forces omp to rediscover every provider — deleting its
// on-disk cache first, so a stale credential or a pool host that just
// came online is not left behind — and replaces the cached listing.
// This is the "Refresh model cache" button under Settings > Agents; a
// window opening never forces this itself, so a slow or unreachable
// provider costs one discovery on expiry, not one per session.
func (a *Agent) RefreshModels(ctx context.Context) ([]Provider, error) {
	return a.models(ctx, true)
}

func (a *Agent) models(ctx context.Context, force bool) ([]Provider, error) {
	modelsMu.Lock()
	defer modelsMu.Unlock()
	if !force && modelsList != nil && time.Since(modelsAt) < modelsTTL {
		return modelsList, nil
	}
	if !a.Status().Ready {
		return nil, fmt.Errorf("omp is not installed yet")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if force {
		for _, f := range []string{"models.db", "models.db-shm", "models.db-wal"} {
			_ = os.Remove(filepath.Join(a.root(), "agent", f))
		}
	}
	cmd := exec.CommandContext(ctx, a.bin(), "models", "--json")
	cmd.Dir = a.StateDir
	cmd.Env = a.env()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("omp models: %s", lastLine(stderr.String()))
	}
	var got struct {
		Models []struct {
			Provider string `json:"provider"`
			ID       string `json:"id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		return nil, fmt.Errorf("omp models: %w", err)
	}
	byName := map[string][]string{}
	for _, m := range got.Models {
		if m.Provider == "" || m.ID == "" {
			continue
		}
		byName[m.Provider] = append(byName[m.Provider], m.ID)
	}
	list := make([]Provider, 0, len(byName))
	for name, ids := range byName {
		sort.Strings(ids)
		list = append(list, Provider{Name: name, Models: ids})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	modelsList, modelsAt = list, time.Now()
	return list, nil
}

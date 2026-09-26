package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/adefesioso/homedash/hub/internal/fleet"
	"github.com/adefesioso/homedash/hub/internal/store"
)

// DefaultProposalRepo is where a proposal goes when Settings names none.
const DefaultProposalRepo = "https://gitea.canica.pe/adefesioso/homedash"

// proposalPrefix marks an issue as an agent's, and is what a duplicate is
// matched on alongside the title.
const proposalPrefix = "[agent proposal] "

// Proposals files an issue on the project's Gitea repository for the hub's
// agent: redacted, capped per day, never twice under one title. The token
// is a setting on the hub and goes nowhere else.
type Proposals struct {
	Store   *store.Store
	Notify  fleet.Notify
	Version string
	Client  *http.Client
}

// ProposalResult is what File did: filed, or linked to an open duplicate.
type ProposalResult struct {
	URL       string `json:"url"`
	Duplicate bool   `json:"duplicate"`
}

// File redacts title and body, refuses past the daily cap, links back to
// an open issue with the same title, and otherwise opens one.
func (p *Proposals) File(ctx context.Context, title, body string) (*ProposalResult, error) {
	title, body = strings.TrimSpace(title), strings.TrimSpace(body)
	if title == "" || body == "" {
		return nil, errors.New("a proposal needs a title and a body")
	}
	token, _ := p.Store.Setting(ctx, "proposals.token")
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("proposals are off on this hub: no Gitea token in Settings")
	}
	api, err := p.api(ctx)
	if err != nil {
		return nil, err
	}
	daily := settingInt(ctx, p.Store, "proposals.daily", 3)
	if n, err := p.Store.ProposalsToday(ctx); err != nil {
		return nil, err
	} else if n >= daily {
		return nil, fmt.Errorf("this hub has filed %d proposals in the last day, the cap; keep it for tomorrow", n)
	}
	redact := p.redactor(ctx)
	title = proposalPrefix + redact(title)
	body = redact(body) + "\n\n---\nFiled by a HomeDash hub's agent (" + p.Version + "); hostnames and addresses redacted."

	if u, err := p.duplicate(ctx, api, strings.TrimSpace(token), title); err != nil {
		return nil, err
	} else if u != "" {
		return &ProposalResult{URL: u, Duplicate: true}, nil
	}
	var issue struct {
		HTMLURL string `json:"html_url"`
	}
	if err := p.call(ctx, http.MethodPost, api+"/issues", strings.TrimSpace(token), map[string]string{"title": title, "body": body}, &issue); err != nil {
		return nil, err
	}
	if err := p.Store.AddProposal(ctx, title, issue.HTMLURL); err != nil {
		return nil, err
	}
	p.Notify("proposal.filed", "", "proposal filed: "+title+" "+issue.HTMLURL)
	return &ProposalResult{URL: issue.HTMLURL}, nil
}

// api turns the repository's web URL into its Gitea API base.
func (p *Proposals) api(ctx context.Context) (string, error) {
	repo, _ := p.Store.Setting(ctx, "proposals.repo")
	if repo = strings.TrimSpace(repo); repo == "" {
		repo = DefaultProposalRepo
	}
	u, err := url.Parse(strings.TrimSuffix(repo, ".git"))
	var parts []string
	if err == nil {
		parts = strings.Split(strings.Trim(u.Path, "/"), "/")
	}
	if err != nil || u.Scheme == "" || u.Host == "" || len(parts) != 2 {
		return "", fmt.Errorf("proposals.repo %q is not a repository URL like %s", repo, DefaultProposalRepo)
	}
	return u.Scheme + "://" + u.Host + "/api/v1/repos/" + parts[0] + "/" + parts[1], nil
}

// duplicate is the link of an open issue with exactly this title, or "".
func (p *Proposals) duplicate(ctx context.Context, api, token, title string) (string, error) {
	var open []struct {
		Title   string `json:"title"`
		HTMLURL string `json:"html_url"`
	}
	q := url.Values{"state": {"open"}, "type": {"issues"}, "q": {strings.TrimPrefix(title, proposalPrefix)}, "limit": {"50"}}
	if err := p.call(ctx, http.MethodGet, api+"/issues?"+q.Encode(), token, nil, &open); err != nil {
		return "", err
	}
	for _, i := range open {
		if strings.EqualFold(strings.TrimSpace(i.Title), title) {
			return i.HTMLURL, nil
		}
	}
	return "", nil
}

func (p *Proposals) call(ctx context.Context, method, u, token string, in, out any) error {
	var body *bytes.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	} else {
		body = bytes.NewReader(nil)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c := p.Client
	if c == nil {
		c = http.DefaultClient
	}
	res, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("the repository did not answer: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("the repository answered %s", res.Status)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// redactor replaces every enrolled host's name and address, the hub's
// address and the space's names with placeholders, whole words only.
func (p *Proposals) redactor(ctx context.Context) func(string) string {
	type pair struct {
		re   *regexp.Regexp
		with string
	}
	var ps []pair
	add := func(word, with string) {
		if word = strings.TrimSpace(word); len(word) >= 2 {
			ps = append(ps, pair{regexp.MustCompile(`(^|[^\w.-])` + regexp.QuoteMeta(word) + `($|[^\w-])`), "${1}" + with + "${2}"})
		}
	}
	hs, _ := p.Store.Hosts(ctx)
	for _, h := range hs {
		add(h.Addr, "<address>")
		add(h.Name, "<host>")
	}
	for k, with := range map[string]string{"hub.lan_addr": "<hub>", "space.name": "<space>", "space.hub_name": "<hub>"} {
		v, _ := p.Store.Setting(ctx, k)
		add(v, with)
	}
	return func(s string) string {
		for _, x := range ps {
			// Twice: adjacent matches share the separator between them.
			s = x.re.ReplaceAllString(x.re.ReplaceAllString(s, x.with), x.with)
		}
		return s
	}
}

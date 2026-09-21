package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is one signed-in connection to one hub.
type Client struct {
	Hub   string
	Token string
	http  *http.Client
}

func newClient(c *Config) *Client {
	return &Client{Hub: strings.TrimRight(c.Hub, "/"), Token: c.Token, http: &http.Client{Timeout: 10 * time.Minute}}
}

// Do makes one API call and returns the body. A JSON body is sent as
// such; a string body as text. The hub's own error text becomes the
// CLI's, and a 401 says what to do about it.
func (c *Client) Do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var rd io.Reader
	ctype := ""
	switch b := body.(type) {
	case nil:
	case string:
		rd, ctype = strings.NewReader(b), "text/plain; charset=utf-8"
	default:
		j, err := json.Marshal(b)
		if err != nil {
			return nil, err
		}
		rd, ctype = bytes.NewReader(j), "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Hub+"/api"+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.Hub, err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errors.New("the hub does not know this session any more; run: homedash login " + c.Hub)
	}
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = resp.Status
		}
		return nil, errors.New(msg)
	}
	return out, nil
}

// get, with a query.
func (c *Client) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	return c.Do(ctx, http.MethodGet, path, nil)
}

func esc(s string) string { return url.PathEscape(s) }

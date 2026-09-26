package store

import "context"

// Proposal is one issue the hub filed on the project's repository.
type Proposal struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Created string `json:"created"`
}

// AddProposal records a filed issue.
func (s *Store) AddProposal(ctx context.Context, title, url string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO proposals(title, url) VALUES (?, ?)`, title, url)
	return err
}

// ProposalsToday counts issues filed in the last 24 hours: the daily cap.
func (s *Store) ProposalsToday(ctx context.Context) (int, error) {
	var n int
	err := s.RO.QueryRowContext(ctx, `SELECT COUNT(*) FROM proposals WHERE created > strftime('%Y-%m-%dT%H:%M:%fZ','now','-1 day')`).Scan(&n)
	return n, err
}

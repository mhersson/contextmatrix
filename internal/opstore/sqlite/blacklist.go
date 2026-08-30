package sqlite

import (
	"context"
	"fmt"
	"time"
)

// RecordIncapableModel upserts a blacklist row keyed by slug. first_seen is
// preserved across reports; reason/sample/last_seen are updated.
func (s *Store) RecordIncapableModel(ctx context.Context, slug, reason, sampleCard, reportedBy string) error {
	now := time.Now().Unix()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO model_blacklist (slug, reason, sample_card, reported_by, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(slug) DO UPDATE SET
			reason=excluded.reason, sample_card=excluded.sample_card,
			reported_by=excluded.reported_by, last_seen=excluded.last_seen`,
		slug, reason, nullable(sampleCard), reportedBy, now, now)
	if err != nil {
		return fmt.Errorf("record incapable model %q: %w", slug, err)
	}

	return nil
}

// BlacklistedSlugs returns every blacklisted OpenRouter slug.
func (s *Store) BlacklistedSlugs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT slug FROM model_blacklist ORDER BY slug`)
	if err != nil {
		return nil, fmt.Errorf("list blacklist: %w", err)
	}

	defer rows.Close() //nolint:errcheck

	var out []string

	for rows.Next() {
		var slug string

		if err := rows.Scan(&slug); err != nil {
			return nil, fmt.Errorf("scan blacklist: %w", err)
		}

		out = append(out, slug)
	}

	return out, rows.Err()
}

// BlacklistEntry is one full model_blacklist row. Timestamps are unix
// seconds.
type BlacklistEntry struct {
	Slug       string
	Reason     string
	SampleCard string
	ReportedBy string
	FirstSeen  int64
	LastSeen   int64
}

// BlacklistEntries returns every blacklist row ordered by slug.
func (s *Store) BlacklistEntries(ctx context.Context) ([]BlacklistEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT slug, reason, COALESCE(sample_card, ''), reported_by, first_seen, last_seen
		FROM model_blacklist ORDER BY slug`)
	if err != nil {
		return nil, fmt.Errorf("list blacklist entries: %w", err)
	}

	defer rows.Close() //nolint:errcheck

	var out []BlacklistEntry

	for rows.Next() {
		var e BlacklistEntry

		if err := rows.Scan(&e.Slug, &e.Reason, &e.SampleCard, &e.ReportedBy, &e.FirstSeen, &e.LastSeen); err != nil {
			return nil, fmt.Errorf("scan blacklist entry: %w", err)
		}

		out = append(out, e)
	}

	return out, rows.Err()
}

// DeleteBlacklistEntry removes one slug from the blacklist. Returns false
// when the slug was not blacklisted.
func (s *Store) DeleteBlacklistEntry(ctx context.Context, slug string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM model_blacklist WHERE slug = ?`, slug)
	if err != nil {
		return false, fmt.Errorf("delete blacklist entry %q: %w", slug, err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete blacklist entry %q: rows affected: %w", slug, err)
	}

	return n > 0, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}

	return s
}

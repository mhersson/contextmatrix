package sqlite

import (
	"context"
	"fmt"
)

// PlaybookPins returns the playbook IDs owner has pinned.
func (s *Store) PlaybookPins(ctx context.Context, owner string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT playbook_id FROM playbook_pins WHERE owner = ?`, owner)
	if err != nil {
		return nil, fmt.Errorf("list playbook pins: %w", err)
	}
	defer rows.Close()

	var ids []string

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan playbook pin: %w", err)
		}

		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list playbook pins: %w", err)
	}

	return ids, nil
}

// SetPlaybookPin pins or unpins a playbook for owner; repeating either is a no-op.
func (s *Store) SetPlaybookPin(ctx context.Context, owner, playbookID string, pinned bool) error {
	query := `DELETE FROM playbook_pins WHERE owner = ? AND playbook_id = ?`
	if pinned {
		query = `INSERT OR IGNORE INTO playbook_pins (owner, playbook_id) VALUES (?, ?)`
	}

	if _, err := s.db.ExecContext(ctx, query, owner, playbookID); err != nil {
		return fmt.Errorf("set playbook pin %q: %w", playbookID, err)
	}

	return nil
}

// DeletePlaybookPins drops every owner's pin on playbookID.
func (s *Store) DeletePlaybookPins(ctx context.Context, playbookID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM playbook_pins WHERE playbook_id = ?`, playbookID); err != nil {
		return fmt.Errorf("delete playbook pins %q: %w", playbookID, err)
	}

	return nil
}

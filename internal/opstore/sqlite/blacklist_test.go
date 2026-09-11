package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestBlacklistRecordAndList(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "ops.db"))
	if err != nil {
		t.Fatal(err)
	}

	defer st.Close()

	ctx := context.Background()

	if err := st.RecordIncapableModel(ctx, "bad/model", "parse failures", "CM-7", "agent:x"); err != nil {
		t.Fatal(err)
	}

	// Idempotent upsert: second report updates, does not duplicate.
	if err := st.RecordIncapableModel(ctx, "bad/model", "no progress", "CM-9", "agent:y"); err != nil {
		t.Fatal(err)
	}

	slugs, err := st.BlacklistedSlugs(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(slugs) != 1 || slugs[0] != "bad/model" {
		t.Fatalf("want [bad/model], got %v", slugs)
	}
}

func TestBlacklistEntries(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "ops.db"))
	if err != nil {
		t.Fatal(err)
	}

	defer st.Close()

	ctx := context.Background()

	if err := st.RecordIncapableModel(ctx, "z/model", "parse failures", "CM-7", "agent:x"); err != nil {
		t.Fatal(err)
	}

	if err := st.RecordIncapableModel(ctx, "a/model", "no progress", "", "agent:y"); err != nil {
		t.Fatal(err)
	}

	entries, err := st.BlacklistEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}

	if entries[0].Slug != "a/model" || entries[1].Slug != "z/model" {
		t.Fatalf("want slug order [a/model z/model], got [%s %s]", entries[0].Slug, entries[1].Slug)
	}

	if entries[0].SampleCard != "" {
		t.Fatalf("want empty sample card for NULL column, got %q", entries[0].SampleCard)
	}

	e := entries[1]
	if e.Reason != "parse failures" || e.SampleCard != "CM-7" || e.ReportedBy != "agent:x" {
		t.Fatalf("unexpected entry fields: %+v", e)
	}

	if e.FirstSeen == 0 || e.LastSeen == 0 {
		t.Fatalf("want non-zero timestamps, got first=%d last=%d", e.FirstSeen, e.LastSeen)
	}
}

func TestDeleteBlacklistEntry(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "ops.db"))
	if err != nil {
		t.Fatal(err)
	}

	defer st.Close()

	ctx := context.Background()

	if err := st.RecordIncapableModel(ctx, "bad/model", "parse failures", "CM-7", "agent:x"); err != nil {
		t.Fatal(err)
	}

	deleted, err := st.DeleteBlacklistEntry(ctx, "bad/model")
	if err != nil {
		t.Fatal(err)
	}

	if !deleted {
		t.Fatal("want deleted=true for existing slug")
	}

	slugs, err := st.BlacklistedSlugs(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(slugs) != 0 {
		t.Fatalf("want empty blacklist after delete, got %v", slugs)
	}

	deleted, err = st.DeleteBlacklistEntry(ctx, "bad/model")
	if err != nil {
		t.Fatal(err)
	}

	if deleted {
		t.Fatal("want deleted=false for absent slug")
	}
}

func TestInsertBlacklistEntry(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "ops.db"))
	if err != nil {
		t.Fatal(err)
	}

	defer st.Close()

	ctx := context.Background()

	inserted, err := st.InsertBlacklistEntry(ctx, "new/model", "blacklisted by operator", "root")
	if err != nil {
		t.Fatal(err)
	}

	if !inserted {
		t.Fatal("first insert must report inserted")
	}

	// An agent report with a sample card, then a manual add for the same
	// slug: the add is a no-op so the report's evidence survives.
	if err := st.RecordIncapableModel(ctx, "bad/model", "parse failures", "CM-7", "agent:x"); err != nil {
		t.Fatal(err)
	}

	inserted, err = st.InsertBlacklistEntry(ctx, "bad/model", "blacklisted by operator", "root")
	if err != nil {
		t.Fatal(err)
	}

	if inserted {
		t.Fatal("insert on a listed slug must report not inserted")
	}

	entries, err := st.BlacklistEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}

	bad := entries[0]
	if bad.Slug != "bad/model" || bad.Reason != "parse failures" || bad.SampleCard != "CM-7" || bad.ReportedBy != "agent:x" {
		t.Fatalf("agent row was touched by the manual add: %+v", bad)
	}

	fresh := entries[1]
	if fresh.Slug != "new/model" || fresh.Reason != "blacklisted by operator" || fresh.SampleCard != "" || fresh.ReportedBy != "root" || fresh.FirstSeen == 0 || fresh.LastSeen != fresh.FirstSeen {
		t.Fatalf("unexpected manual row: %+v", fresh)
	}
}

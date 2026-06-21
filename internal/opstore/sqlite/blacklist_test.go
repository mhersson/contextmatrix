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

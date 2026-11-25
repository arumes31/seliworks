//go:build integration

package app

import (
	"context"
	"testing"
)

func TestRetentionPurgesOnlyExpiredSecurityData(t *testing.T) {
	a, _ := invitationSecurityFixture(t)
	ctx := context.Background()
	if _, err := a.DB.Exec(ctx, `UPDATE organization SET security_retention_days=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(ctx, `INSERT INTO auth_limits(bucket,window_start,attempts) VALUES('old',clock_timestamp()-interval '2 days',1),('fresh',clock_timestamp(),1)`); err != nil {
		t.Fatal(err)
	}
	deleted, err := a.purgeExpired(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	rows, err := a.DB.Query(ctx, `SELECT bucket FROM auth_limits WHERE bucket IN ('old','fresh')`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var b string
		if err = rows.Scan(&b); err != nil {
			t.Fatal(err)
		}
		left = append(left, b)
	}
	rows.Close()
	if deleted["auth_limits"] < 1 || len(left) != 1 || left[0] != "fresh" {
		t.Fatalf("deleted %v, remaining %v", deleted, left)
	}
	var audits int
	if err = a.DB.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action='retention.security_cleaned_automatically'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("audit events %d %v", audits, err)
	}
}

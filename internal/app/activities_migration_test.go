//go:build integration

package app

import (
	"context"
	"testing"
)

func TestGermanActivitiesMigration(t *testing.T) {
	a, _ := invitationSecurityFixture(t)
	ctx := context.Background()
	var names []string
	if err := a.DB.QueryRow(ctx, `SELECT array_agg(name ORDER BY name) FROM activities`).Scan(&names); err != nil {
		t.Fatal(err)
	}
	want := []string{"Erntearbeit", "Feldarbeit", "Maschinenwartung", "Sonstiges", "Stallarbeit", "Tiere füttern", "Transport"}
	if len(names) != len(want) {
		t.Fatalf("got %v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("got %v, want %v", names, want)
		}
	}
	// Simulate an existing installation with untouched and customized rows.
	if _, err := a.DB.Exec(ctx, `DELETE FROM activities; INSERT INTO activities(name,version) VALUES ('Field work',1),('Other',2),('Meine Tätigkeit',1); DELETE FROM schema_migrations WHERE name='migrations/012_german_activities.sql'`); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := a.DB.QueryRow(ctx, `SELECT id::text FROM activities WHERE name='Field work'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Migrate(ctx, a.DB); err != nil {
			t.Fatal(err)
		}
	}
	var valid bool
	if err := a.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM activities WHERE id=$1 AND name='Feldarbeit' AND version=2) AND EXISTS(SELECT 1 FROM activities WHERE name='Other' AND version=2) AND EXISTS(SELECT 1 FROM activities WHERE name='Meine Tätigkeit' AND version=1)`, id).Scan(&valid); err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Fatal("migration changed customized data, identities, or reran")
	}
}

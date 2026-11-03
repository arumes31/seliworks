//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func pauseHistoryRequest(t *testing.T, a *App, user User, path, key string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw := token()
	if _, err := a.DB.Exec(context.Background(), `INSERT INTO auth_sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,'pause-test',clock_timestamp()+interval '1 hour')`, digest(raw), user.ID); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, a.Config.AppURL+"/api/v1"+path, bytes.NewReader(data))
	r.Header.Set("Origin", a.Config.AppURL)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", "pause-test")
	r.Header.Set("Idempotency-Key", key)
	r.AddCookie(&http.Cookie{Name: "seliworks_session", Value: raw})
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}

func seedPauses(t *testing.T, a *App, user User, id string, n int) {
	t.Helper()
	if _, err := a.DB.Exec(context.Background(), `INSERT INTO work_pauses(session_id,started_at,resumed_at,actor_id)
SELECT $1,'2026-01-02T09:00:00Z'::timestamptz+make_interval(mins=>i*2),'2026-01-02T09:00:00Z'::timestamptz+make_interval(mins=>i*2+1),$2 FROM generate_series(1,$3) i`, id, user.ID, n); err != nil {
		t.Fatal(err)
	}
}

func TestPauseHistoryIsBounded(t *testing.T) {
	a, user := invitationSecurityFixture(t)
	id, _ := deleteWorkEntry(t, a, user, "RUNNING")
	seedPauses(t, a, user, id, maxPausesPerSession)
	w := pauseHistoryRequest(t, a, user, "/work-sessions/"+id+"/pause", "pause-limit-key", map[string]any{})
	if w.Code != http.StatusConflict || !bytes.Contains(w.Body.Bytes(), []byte("PAUSE_LIMIT")) {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var pauses int
	if err := a.DB.QueryRow(context.Background(), `SELECT count(*) FROM work_pauses WHERE session_id=$1`, id).Scan(&pauses); err != nil || pauses != maxPausesPerSession {
		t.Fatalf("pause recorded beyond the limit: %d, %v", pauses, err)
	}
}

func TestPauseHistoryReceiptsStayCompact(t *testing.T) {
	a, user := invitationSecurityFixture(t)
	id, _ := deleteWorkEntry(t, a, user, "RUNNING")
	seedPauses(t, a, user, id, 20)
	w := pauseHistoryRequest(t, a, user, "/work-sessions/"+id+"/pause", "pause-receipt-key", map[string]any{})
	if w.Code != http.StatusOK {
		t.Fatalf("pause status %d: %s", w.Code, w.Body.String())
	}
	var first map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil || first["id"] != id {
		t.Fatalf("pause response %v: %s", err, w.Body.String())
	}
	var stored []byte
	if err := a.DB.QueryRow(context.Background(), `SELECT result FROM commands WHERE operation=$1 AND key='pause-receipt-key'`, "work.pause:"+id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	var receipt map[string]any
	if err := json.Unmarshal(stored, &receipt); err != nil || len(receipt) != 1 || receipt["session_id"] != id {
		t.Fatalf("receipt is not a compact session reference: %s", stored)
	}
	replay := pauseHistoryRequest(t, a, user, "/work-sessions/"+id+"/pause", "pause-receipt-key", map[string]any{})
	var again map[string]any
	if replay.Code != http.StatusOK || json.Unmarshal(replay.Body.Bytes(), &again) != nil || again["id"] != id {
		t.Fatalf("replay status %d: %s", replay.Code, replay.Body.String())
	}
	var pauses int
	if err := a.DB.QueryRow(context.Background(), `SELECT count(*) FROM work_pauses WHERE session_id=$1`, id).Scan(&pauses); err != nil || pauses != 21 {
		t.Fatalf("replay changed pause history: %d, %v", pauses, err)
	}
}

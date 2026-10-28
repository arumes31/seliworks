package app

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The primary activity remains the pricing/grouping key. Aggregate the other
// activities before joining so a work interval never contributes time twice.
const workActivitiesJoin = ` LEFT JOIN LATERAL (
 SELECT array_agg(a.id::text ORDER BY (a.id=s.activity_id) DESC,a.name,a.id) AS ids,
 array_agg(a.name ORDER BY (a.id=s.activity_id) DESC,a.name,a.id) AS names
 FROM activities a WHERE a.id=s.activity_id OR a.id IN
 (SELECT activity_id FROM work_session_activities WHERE session_id=s.id)
) wa ON true `

type workActivityInput struct {
	ActivityID           string   `json:"activity_id"`
	ActivityIDs          []string `json:"activity_ids"`
	AdditionalActivities *string  `json:"additional_activities"`
}

// Existing callers may omit the new fields. Resolve and validate under the
// caller's work-session lock, then write together with the versioned work edit.
func (in *workActivityInput) resolve(ctx context.Context, tx pgx.Tx, id string, requirePrimary bool) error {
	if len(in.ActivityIDs) > 50 {
		return problem(422, "VALIDATION", "Choose at most 50 activities")
	}
	if in.ActivityID != "" {
		parsed, err := uuid.Parse(in.ActivityID)
		if err != nil || parsed.String() != strings.ToLower(in.ActivityID) {
			return problem(422, "VALIDATION", "Choose a valid primary activity")
		}
		in.ActivityID = parsed.String()
	}
	if in.ActivityIDs == nil {
		in.ActivityIDs = []string{}
		if id != "" {
			if err := tx.QueryRow(ctx, `SELECT coalesce(array_agg(activity_id::text),'{}'::text[]) FROM work_session_activities WHERE session_id=$1`, id).Scan(&in.ActivityIDs); err != nil {
				return err
			}
		}
		if in.ActivityID != "" && !slices.Contains(in.ActivityIDs, in.ActivityID) {
			in.ActivityIDs = append(in.ActivityIDs, in.ActivityID)
		}
	}
	seen := map[string]bool{}
	for i, value := range in.ActivityIDs {
		parsed, err := uuid.Parse(value)
		if err != nil || parsed.String() != strings.ToLower(value) || seen[parsed.String()] {
			return problem(422, "VALIDATION", "Choose distinct valid activities")
		}
		in.ActivityIDs[i] = parsed.String()
		seen[parsed.String()] = true
	}
	if (requirePrimary && in.ActivityID == "") || (in.ActivityID != "" && !seen[in.ActivityID]) || (in.ActivityID == "" && len(seen) > 0) {
		return problem(422, "ACTIVITY_REQUIRED", "Choose a primary activity from the selected activities")
	}
	var valid int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM activities a WHERE a.id=ANY($1::uuid[]) AND (a.active OR a.id IN (SELECT activity_id FROM work_session_activities WHERE session_id=NULLIF($2,'')::uuid UNION SELECT activity_id FROM work_sessions WHERE id=NULLIF($2,'')::uuid))`, in.ActivityIDs, id).Scan(&valid); err != nil {
		return err
	}
	if valid != len(in.ActivityIDs) {
		return problem(422, "ACTIVITY_REQUIRED", "Choose active activities or keep previously selected activities")
	}
	if in.AdditionalActivities == nil {
		text := ""
		if id != "" {
			if err := tx.QueryRow(ctx, `SELECT additional_activities FROM work_sessions WHERE id=$1`, id).Scan(&text); err != nil {
				return err
			}
		}
		in.AdditionalActivities = &text
	}
	text := strings.TrimSpace(*in.AdditionalActivities)
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) || utf8.RuneCountInString(text) > 1000 {
		return problem(422, "VALIDATION", "Additional activities must contain at most 1000 characters")
	}
	in.AdditionalActivities = &text
	return nil
}

func (in workActivityInput) save(ctx context.Context, tx pgx.Tx, id string) error {
	if _, err := tx.Exec(ctx, `UPDATE work_sessions SET additional_activities=$2 WHERE id=$1`, id, *in.AdditionalActivities); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM work_session_activities WHERE session_id=$1`, id); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO work_session_activities(session_id,activity_id) SELECT $1,id FROM unnest($2::uuid[]) id WHERE id<>NULLIF($3,'')::uuid`, id, in.ActivityIDs, in.ActivityID)
	return err
}

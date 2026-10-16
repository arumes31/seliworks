package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"
)

func secretPassword(env string) (string, error) {
	p := os.Getenv(env)
	if p == "" {
		return "", errors.New(env + " is required; passwords are read from a file")
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return "", errors.New("cannot read password file")
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}
func (a *App) Bootstrap(ctx context.Context, reset bool) error {
	email := strings.ToLower(strings.TrimSpace(os.Getenv("BOOTSTRAP_EMAIL")))
	if !validEmail(email) {
		return errors.New("valid BOOTSTRAP_EMAIL is required")
	}
	password, e := secretPassword("BOOTSTRAP_PASSWORD_FILE")
	if e != nil {
		return e
	}
	hash, e := hashPassword(password)
	if e != nil {
		return e
	}
	tx, e := a.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(736954821)`); e != nil {
		return e
	}
	var id string
	if reset {
		e = tx.QueryRow(ctx, `UPDATE users SET password_hash=$2,version=version+1 WHERE email=$1 RETURNING id::text`, email, hash).Scan(&id)
		if e == nil {
			_, e = tx.Exec(ctx, `DELETE FROM auth_sessions WHERE user_id=$1`, id)
		}
		if e == nil {
			_, e = tx.Exec(ctx, `DELETE FROM user_totp WHERE user_id=$1`, id)
		}
		if e == nil {
			_, e = tx.Exec(ctx, `DELETE FROM recovery_codes WHERE user_id=$1`, id)
		}
	} else {
		var n int
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); e != nil {
			return e
		}
		if n != 0 {
			return errors.New("bootstrap already completed; use the authenticated admin interface")
		}
		name := envDefault("BOOTSTRAP_NAME", "Administrator")
		if !checkName(name) {
			return errors.New("invalid BOOTSTRAP_NAME")
		}
		e = tx.QueryRow(ctx, `INSERT INTO users(email,name,password_hash,is_admin,is_worker) VALUES($1,$2,$3,true,true) RETURNING id::text`, email, name, hash).Scan(&id)
	}
	if e != nil {
		return e
	}
	action := "auth.bootstrap"
	if reset {
		action = "auth.assisted_password_reset"
	}
	if e = a.audit(ctx, tx, id, action, id, nil, nil); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (a *App) Demo(ctx context.Context) error {
	if a.Config.Environment != "development" {
		return errors.New("demo data requires APP_ENV=development")
	}
	password, e := secretPassword("DEMO_PASSWORD_FILE")
	if e != nil {
		return e
	}
	hash, e := hashPassword(password)
	if e != nil {
		return e
	}
	tx, e := a.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(736954821)`); e != nil {
		return e
	}
	var n int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		return errors.New("demo requires an empty deployment; existing data is never overwritten")
	}
	var admin, worker, farm, other, activity string
	if e = tx.QueryRow(ctx, `INSERT INTO users(name,email,password_hash,is_admin) VALUES('Alex Berger','demo@seliworks.local',$1,true) RETURNING id::text`, hash).Scan(&admin); e != nil {
		return e
	}
	if e = tx.QueryRow(ctx, `INSERT INTO users(name,email,password_hash) VALUES('Lena Gruber','worker@seliworks.local',$1) RETURNING id::text`, hash).Scan(&worker); e != nil {
		return e
	}
	if e = tx.QueryRow(ctx, `INSERT INTO farms(name,address,email) VALUES('Sonnenhof','Dorfstraße 12, 5020 Salzburg','office@sonnenhof.example') RETURNING id::text`).Scan(&farm); e != nil {
		return e
	}
	if e = tx.QueryRow(ctx, `INSERT INTO farms(name,address) VALUES('Gruber Biohof','Feldweg 7, 5400 Hallein') RETURNING id::text`).Scan(&other); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO worker_farms VALUES($1,$3),($1,$4),($2,$3),($2,$4)`, admin, worker, farm, other); e != nil {
		return e
	}
	if e = tx.QueryRow(ctx, `SELECT id::text FROM activities WHERE name='Field work'`).Scan(&activity); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO rates(hourly_rate,effective_from) VALUES(32.50,'2020-01-01T00:00:00Z'); UPDATE organization SET name='SeliWorks · Demo',address='Bergstraße 4, 5020 Salzburg',email='hello@seliworks.example',bank_details='DEMO — configure your bank account',tax_confirmed=false`); e != nil {
		return e
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return e
	}
	loc, e := time.LoadLocation("Europe/Vienna")
	if e != nil {
		return e
	}
	d := now.In(loc)
	for i := 1; i <= 10; i++ {
		start := time.Date(d.Year(), d.Month(), d.Day()-i, 7, 30, 0, 0, loc)
		stop := start.Add(time.Duration(4+i%4)*time.Hour + 30*time.Minute)
		fid := farm
		if i%3 == 0 {
			fid = other
		}
		uid := worker
		if i%2 == 0 {
			uid = admin
		}
		state := "SUBMITTED"
		description := "Prepared the field and checked irrigation equipment."
		if i == 1 {
			state = "STOPPED_PENDING_DETAILS"
			description = ""
		}
		if _, e = tx.Exec(ctx, `INSERT INTO work_sessions(worker_id,farm_id,activity_id,started_at,stopped_at,effective_start,effective_stop,state,description,location,created_by,updated_at) VALUES($1,$2,$3,$4,$5,$4,$5,$6,$7,'North field',$8,$5)`, uid, fid, activity, start, stop, state, description, admin); e != nil {
			return e
		}
	}
	if e = a.audit(ctx, tx, admin, "demo.created", "organization", nil, map[string]bool{"synthetic": true}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

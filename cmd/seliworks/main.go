package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"seliworks/internal/app"
	"syscall"
	"time"
)

func main() {
	if e := run(); e != nil {
		slog.Error("SeliWorks stopped", "error", e.Error())
		os.Exit(1)
	}
}
func run() error {
	var level slog.Level
	configuredLevel := os.Getenv("LOG_LEVEL")
	if configuredLevel == "" {
		configuredLevel = "info"
	}
	if e := level.UnmarshalText([]byte(configuredLevel)); e != nil {
		return errors.New("LOG_LEVEL must be debug, info, warn or error")
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
	c, e := app.LoadConfig()
	if e != nil {
		return e
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	db, e := app.OpenDB(ctx, c.DatabaseURL)
	if e != nil {
		return e
	}
	defer db.Close()
	if e = db.Ping(ctx); e != nil {
		return errors.New("database connection failed")
	}
	a := &app.App{DB: db, Config: c}
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	switch command {
	case "migrate":
		return app.Migrate(ctx, db)
	case "bootstrap":
		return a.Bootstrap(ctx, false)
	case "reset-password":
		return a.Bootstrap(ctx, true)
	case "demo":
		return a.Demo(ctx)
	case "serve":
	default:
		return errors.New("unknown command; use serve, migrate, bootstrap, reset-password or demo")
	}
	// Serving never runs migrations or creates users implicitly.
	if e = app.SchemaReady(ctx, db); e != nil {
		return errors.New("schema missing or outdated: run migrate before serve")
	}
	if c.SMTPHost != "" {
		go a.RunMail(ctx)
	}
	s := &http.Server{Addr: c.ListenAddr, Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 16384}
	done := make(chan error, 1)
	go func() { slog.Info("SeliWorks listening", "address", c.ListenAddr); done <- s.ListenAndServe() }()
	select {
	case e := <-done:
		if e != http.ErrServerClosed {
			return e
		}
		return nil
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return s.Shutdown(shutdown)
	}
}

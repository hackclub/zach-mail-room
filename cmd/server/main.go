// Command server runs the zach-mail-room web app: JSON API + PWA static files.
// All configuration comes from environment variables (see .env.example).
package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hackclub/zach-mail-room/internal/auth"
	"github.com/hackclub/zach-mail-room/internal/authors"
	"github.com/hackclub/zach-mail-room/internal/config"
	"github.com/hackclub/zach-mail-room/internal/db"
	"github.com/hackclub/zach-mail-room/internal/httpapi"
	"github.com/hackclub/zach-mail-room/internal/store"
	"github.com/hackclub/zach-mail-room/internal/theseus"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := db.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return err
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	var dir authors.Directory = authors.Static{}
	if cfg.AirtableAPIKey != "" {
		dir = authors.NewAirtable(authors.AirtableConfig{
			APIKey: cfg.AirtableAPIKey, BaseID: cfg.AirtableAuthorsBaseID,
			Table: cfg.AirtableAuthorsTable, EmailField: cfg.AirtableAuthorsEmailFld,
		})
	} else {
		log.Warn("AIRTABLE_API_KEY not set: nobody will be recognized as a YSWS author (admins still are)")
	}

	var static fs.FS
	if cfg.StaticDir != "" {
		if st, err := os.Stat(cfg.StaticDir); err == nil && st.IsDir() {
			static = os.DirFS(cfg.StaticDir)
		} else {
			log.Warn("static dir missing; serving API only (run `make web`)", "dir", cfg.StaticDir)
		}
	}

	h := httpapi.New(httpapi.Deps{
		Config: cfg,
		Store:  store.New(pool),
		Provider: auth.NewHCA(auth.HCAConfig{
			BaseURL: cfg.HCABaseURL, ClientID: cfg.HCAClientID, ClientSecret: cfg.HCAClientSecret,
			Scopes: cfg.HCAScopes,
		}),
		Authors:   dir,
		Warehouse: theseus.New(cfg.TheseusBaseURL, cfg.TheseusAPIKey, nil),
		Static:    static,
		Log:       log,
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr, "origins", cfg.Origins())
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}

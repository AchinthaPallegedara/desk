// Command server runs the Claviq Desk backend (REST API + MCP + OAuth) and its admin tasks.
//
//	server                         start the HTTP server
//	server migrate <cmd>           run database migrations (up, down, redo, status, version)
//	server create-tenant           create a business and its owner (prompts for details)
//	server set-password <email>    reset an owner's password
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/claviq/desk/backend/internal/auth"
	"github.com/claviq/desk/backend/internal/config"
	"github.com/claviq/desk/backend/internal/database"
	"github.com/claviq/desk/backend/internal/domain"
	"github.com/claviq/desk/backend/internal/httpapi"
	"github.com/claviq/desk/backend/internal/mcpserver"
	"github.com/claviq/desk/backend/internal/models"
	"github.com/claviq/desk/backend/internal/render"
	"github.com/claviq/desk/backend/internal/storage"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return serve()
	}
	switch args[0] {
	case "migrate":
		cmd := "up"
		if len(args) > 1 {
			cmd = args[1]
		}
		dsn := os.Getenv("MIGRATE_DATABASE_URL")
		if dsn == "" {
			return errors.New("MIGRATE_DATABASE_URL is required")
		}
		return database.Migrate(dsn, cmd)
	case "create-tenant":
		return createTenant(args[1:])
	case "set-password":
		return setPassword(args[1:])
	case "serve":
		return serve()
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func newLogger(prod bool) *slog.Logger {
	if prod {
		return slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, nil))
}

func serve() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg.IsProduction())

	db, err := database.Open(cfg.DatabaseURL, false)
	if err != nil {
		return err
	}
	store, err := storage.New(cfg)
	if err != nil {
		return err
	}
	svc := domain.New(domain.Options{
		DB: db, Renderer: render.New(cfg.TypstBin), Storage: store, DataDir: cfg.DataDir,
		AppSecret: cfg.AppSecret, PublicURL: cfg.PublicURL, Logger: log,
	})
	if err := svc.SyncBundledFonts(context.Background()); err != nil {
		return fmt.Errorf("sync bundled fonts: %w", err)
	}

	sessions := auth.NewSessions(db, cfg.AppSecret, cfg.SecureCookies())
	oauth := auth.NewOAuth(db, sessions, cfg.PublicURL, cfg.AppSecret, log)
	mcpSrv := mcpserver.New(svc, log)

	router := httpapi.NewRouter(httpapi.Deps{
		DB: db, Service: svc, Sessions: sessions, OAuth: oauth,
		MCPHandler: mcpserver.Handler(mcpSrv, oauth), PublicURL: cfg.PublicURL, Logger: log,
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: MCP uses long-lived streaming responses.
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Housekeeping: expired OAuth tokens.
	go func() {
		t := time.NewTicker(6 * time.Hour)
		defer t.Stop()
		for {
			if n, err := oauth.Store().Cleanup(ctx); err != nil {
				log.Warn("oauth cleanup", "error", err)
			} else if n > 0 {
				log.Info("oauth cleanup", "deleted", n)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr, "env", cfg.Env, "public_url", cfg.PublicURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
	return nil
}

// ---------- admin commands (owner DB connection) ----------

func ownerDB() (*database.DBHandle, error) {
	dsn := os.Getenv("MIGRATE_DATABASE_URL")
	if dsn == "" {
		return nil, errors.New("MIGRATE_DATABASE_URL is required (tenant creation needs the schema owner)")
	}
	db, err := database.Open(dsn, false)
	if err != nil {
		return nil, err
	}
	return &database.DBHandle{DB: db}, nil
}

func prompt(r *bufio.Reader, label string) string {
	fmt.Print(label)
	s, _ := r.ReadString('\n')
	return strings.TrimSpace(s)
}

func readPassword(label string) (string, error) {
	if v := os.Getenv("OWNER_PASSWORD"); v != "" {
		return v, nil
	}
	fmt.Print(label)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return string(b), err
}

func createTenant(args []string) error {
	fs := flag.NewFlagSet("create-tenant", flag.ExitOnError)
	name := fs.String("name", "", "business name")
	email := fs.String("email", "", "owner email")
	_ = fs.Parse(args)

	in := bufio.NewReader(os.Stdin)
	if *name == "" {
		*name = prompt(in, "Business name: ")
	}
	if *email == "" {
		*email = prompt(in, "Owner email: ")
	}
	pw, err := readPassword("Owner password (min 10 chars): ")
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	h, err := ownerDB()
	if err != nil {
		return err
	}
	t, u, err := domain.BootstrapTenant(context.Background(), h.DB, domain.NewTenant{BusinessName: *name, OwnerEmail: *email, PasswordHash: hash})
	if err != nil {
		return err
	}
	fmt.Printf("Created tenant %q (%s) with owner %s\n", t.Name, t.ID, u.Email)
	return nil
}

func setPassword(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: server set-password <email>")
	}
	pw, err := readPassword("New password (min 10 chars): ")
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	h, err := ownerDB()
	if err != nil {
		return err
	}
	res := h.DB.Model(&models.User{}).Where("email = ?", strings.ToLower(args[0])).Update("password_hash", hash)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("no user with email %s", args[0])
	}
	fmt.Println("Password updated; existing sessions are signed out.")
	return nil
}

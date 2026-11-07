package main

import (
	"context"
	_ "embed"
	"flag"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"database/sql"

	"github.com/adrg/xdg"
	"github.com/ajeetdsouza/clidle/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	wtea "github.com/charmbracelet/wish/bubbletea"
	"github.com/pkg/errors"

	"golang.org/x/exp/slog"
	_ "modernc.org/sqlite"
)

var (
	// pathClidle is the path to the local data directory.
	// This is usually set to ~/.local/share/clidle on most UNIX systems.
	pathClidle  string
	pathStore   string
	pathHostKey string

	//go:embed schema.sql
	schemaSQL string

	// Default Bubbletea options.
	teaOptions []tea.ProgramOption = []tea.ProgramOption{
		tea.WithAltScreen(),
		tea.WithOutput(os.Stderr),
	}
)

func init() {
	pathClidle = os.Getenv("CLIDLE_DATA_DIR")
	if pathClidle == "" {
		pathClidle = filepath.Join(xdg.DataHome, "clidle")
	}

	pathStore = filepath.Join(pathClidle, "clidle.db")
	pathHostKey = filepath.Join(pathClidle, "hostkey")
}

func main() {
	flagServe := flag.String("serve", "", "Spawns an SSH server on the given address (format: 0.0.0.0:1337)")
	flag.Parse()

	var err error
	if addr := *flagServe; addr != "" {
		err = runServer(addr)
	} else {
		err = runCLI()
	}
	if err != nil {
		slog.Error("error running application", "error", slog.Any("error", err))
		os.Exit(1)
	}
}

func runCLI() error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	model := newModel(ctx, store.New(db), EnglishDictionary)
	program := tea.NewProgram(model, teaOptions...)

	_, err = program.Run()
	return err
}

func runServer(addr string) error {
	db, err := openDB()
	if err != nil {
		return errors.Wrapf(err, "could not open database")
	}
	defer func() { _ = db.Close() }()

	queries := store.New(db)
	server, err := wish.NewServer(
		wish.WithAddress(addr),
		wish.WithIdleTimeout(30*time.Minute),
		wish.WithMiddleware(
			wtea.Middleware(func(session ssh.Session) (tea.Model, []tea.ProgramOption) {
				pty, _, active := session.Pty()
				if !active {
					wish.Fatalf(session, "no active terminal, skipping")
					return nil, nil
				}

				ctx := session.Context()
				model := newModel(ctx, queries, EnglishDictionary)
				model.windowWidth = pty.Window.Width
				model.windowHeight = pty.Window.Height

				return model, teaOptions
			}),
		),
		wish.WithHostKeyPath(pathHostKey),
	)
	if err != nil {
		return errors.Wrapf(err, "could not create server")
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	slog.Info("starting server", slog.String("address", server.Addr))
	go func() {
		if err := server.ListenAndServe(); err != nil {
			slog.Error("server returned an error", slog.Any("error", err))
			done <- os.Interrupt
		}
	}()

	<-done
	slog.Info("stopping server")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = server.Shutdown(ctx)
	return errors.Wrapf(err, "could not shutdown server")
}

func openDB() (*sql.DB, error) {
	if err := os.MkdirAll(pathClidle, 0700); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", pathStore)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite does not support concurrent writes

	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

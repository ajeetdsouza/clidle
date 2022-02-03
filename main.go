package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/wish"
	bm "github.com/charmbracelet/wish/bubbletea"
	lm "github.com/charmbracelet/wish/logging"
	"github.com/gliderlabs/ssh"
)

func main() {
	flag.Parse()
	if addr := *flagServe; addr != "" {
		runServer(addr)
	} else {
		runCli()
	}
}

func runCli() {
	model := &model{}
	program := tea.NewProgram(model, teaOptions...)

	exitCode := 0
	if err := program.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "clidle: %s\n", err)
		exitCode = 1
	}
	for _, err := range model.errors {
		fmt.Fprintf(os.Stderr, "clidle: %s\n", err)
		exitCode = 1
	}
	os.Exit(exitCode)
}

func runServer(addr string) {
	withHostKey := wish.WithHostKeyPath(pathHostKey)
	if pem, ok := os.LookupEnv(envHostKey); ok {
		withHostKey = wish.WithHostKeyPEM([]byte(pem))
	}

	server, err := wish.NewServer(
		wish.WithAddress(addr),
		wish.WithIdleTimeout(30*time.Minute),
		wish.WithMiddleware(
			bm.Middleware(func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
				pty, _, active := s.Pty()
				if !active {
					log.Printf("no active terminal, skipping")
					return nil, nil
				}
				model := &model{
					width:  pty.Window.Width,
					height: pty.Window.Height,
				}
				return model, teaOptions
			}),
			lm.Middleware(),
		),
		withHostKey,
	)
	if err != nil {
		log.Fatalf("could not create server: %s", err)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	log.Printf("starting server: %s", server.Addr)
	go func() {
		if err := server.ListenAndServe(); err != nil {
			log.Fatalf("server returned an error: %s", err)
		}
	}()

	<-done
	log.Println("stopping server...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("could not shutdown server gracefully: %s", err)
	}
}

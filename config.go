package main

import (
	"flag"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	envHostKey = "_CLIDLE_HOSTKEY"
)

var (
	// pathClidle is the path to the local data directory.
	// This is usually set to ~/.local/share/clidle on most UNIX systems.
	pathClidle  = filepath.Join(xdg.DataHome, "clidle")
	pathDb      = filepath.Join(pathClidle, "db.json")
	pathHostKey = filepath.Join(pathClidle, "hostkey")

	flagServe = flag.String("serve", "", "Spawns an SSH server on the given address (format: 0.0.0.0:1337)")

	teaOptions = []tea.ProgramOption{tea.WithAltScreen(), tea.WithOutput(os.Stderr)}
)

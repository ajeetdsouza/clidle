package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	exitCode := run()
	os.Exit(exitCode)
}

func run() int {
	model := &model{}
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithOutput(os.Stderr))

	exitCode := 0
	if err := program.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "clidle: %s\n", err)
		exitCode = 1
	}
	for _, err := range model.errors {
		fmt.Fprintf(os.Stderr, "clidle: %s\n", err)
		exitCode = 1
	}
	return exitCode
}

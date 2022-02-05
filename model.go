package main

import (
	"bytes"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	// numGuesses is the maximum number of guesses you can make.
	numGuesses = 6
	// numChars is the word size in characters.
	numChars = 5
)

type model struct {
	score     int
	word      [numChars]byte
	gameOver  bool
	errors    []error
	keyStates map[byte]keyState

	status        string
	statusPending int

	height int
	width  int

	grid    [numGuesses][numChars]byte
	gridRow int
	gridCol int
}

var _ tea.Model = (*model)(nil)

func (m *model) Init() tea.Cmd {
	m.keyStates = make(map[byte]keyState, 26)
	return m.withDb(func(db *db) {
		m.score = db.score()
		m.reset()
	})
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case msgResetStatus:
		// If there is more than one pending status message, that means
		// something else is currently displaying a status message, so we don't
		// want to overwrite it.
		m.statusPending--
		if m.statusPending == 0 {
			m.resetStatus()
		}
		return m, nil
	case tea.KeyMsg:
		// If any key is pressed, reset the status message.
		m.resetStatus()

		switch msg.Type {
		case tea.KeyCtrlC:
			return m, m.doExit()
		case tea.KeyCtrlR:
			m.reset()
			return m, nil
		case tea.KeyBackspace:
			return m, m.doDeleteChar()
		case tea.KeyEnter:
			if m.gameOver {
				m.reset()
				return m, nil
			}
			return m, m.doAcceptWord()
		case tea.KeyRunes:
			if len(msg.Runes) == 1 {
				return m, m.doAcceptChar(msg.Runes[0])
			}
		}
	// If the window is resized, store its new dimensions.
	case tea.WindowSizeMsg:
		return m, m.doResize(msg)
	}
	return m, nil
}

func (m *model) View() string {
	status := m.viewStatus()
	grid := m.viewGrid()
	keyboard := m.viewKeyboard()

	// Truncate the status if it is too long.
	if len(status) > m.width && m.width > 3 {
		status = status[:m.width-3] + "..."
	}

	// Drop the keyboard if it doesn't fit.
	height := lipgloss.Height(status) + lipgloss.Height(grid) + lipgloss.Height(keyboard)
	width := lipgloss.Width(keyboard)
	if width < lipgloss.Width(status) || width < lipgloss.Width(grid) {
		width = 0
	}
	if m.height < height || m.width < width {
		keyboard = ""
	}

	game := lipgloss.JoinVertical(lipgloss.Center, status, grid, keyboard, _controls)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, game)
}

func (m *model) reset() {
	// Unlock and reset the grid.
	m.gameOver = false
	m.gridCol = 0
	m.gridRow = 0

	// Clear the key state.
	for k := range m.keyStates {
		delete(m.keyStates, k)
	}

	// Set the puzzle word.
	word := getWord()
	copy(m.word[:], word)

	// Reset the status message.
	m.resetStatus()
}

// setStatus sets the status message, and returns a tea.Cmd that restores the
// default status message after a delay.
func (m *model) setStatus(msg string, duration time.Duration) tea.Cmd {
	m.status = msg
	if duration > 0 {
		m.statusPending++
		return tea.Tick(duration, func(time.Time) tea.Msg {
			return msgResetStatus{}
		})
	}
	return nil
}

// resetStatus immediately resets the status message to its default value.
func (m *model) resetStatus() {
	m.status = fmt.Sprintf("Score: %d", m.score)
}

// doAcceptWord accepts the current word.
func (m *model) doAcceptWord() tea.Cmd {
	if m.gameOver {
		return nil
	}

	// Only accept a word if it is complete.
	if m.gridCol != numChars {
		return m.setStatus("Your guess must be a 5-letter word.", 1*time.Second)
	}

	// Check if the input word is valid.
	word := m.grid[m.gridRow]
	if !isWord(string(word[:])) {
		return m.setStatus("That's not a valid word.", 1*time.Second)
	}

	// Update the state of the used letters.
	success := true
	for i := 0; i < numChars; i++ {
		key := word[i]
		keyStatus := keyStateAbsent
		if key == m.word[i] {
			keyStatus = keyStateCorrect
		} else {
			success = false
			if bytes.IndexByte(m.word[:], key) != -1 {
				keyStatus = keyStatePresent
			}
		}
		if m.keyStates[key] < keyStatus {
			m.keyStates[key] = keyStatus
		}
	}

	// Move to the next row.
	m.gridRow++
	m.gridCol = 0

	// Check if the game is over.
	if success {
		return m.doWin()
	} else if m.gridRow == numGuesses {
		return m.doLoss()
	}
	return nil
}

// doAcceptChar adds one input character to the current word.
func (m *model) doAcceptChar(ch rune) tea.Cmd {
	// Only accept a character if the current word is incomplete.
	if m.gameOver || !(m.gridRow < numGuesses && m.gridCol < numChars) {
		return nil
	}

	ch = toAsciiUpper(ch)
	if isAsciiUpper(ch) {
		m.grid[m.gridRow][m.gridCol] = byte(ch)
		m.gridCol++
	}
	return nil
}

// doDeleteChar deletes the last character in the current word.
func (m *model) doDeleteChar() tea.Cmd {
	if !m.gameOver && m.gridCol > 0 {
		m.gridCol--
	}
	return nil
}

// doExit exits the program.
func (*model) doExit() tea.Cmd {
	return tea.Quit
}

// doResize updates the size of the window.
func (m *model) doResize(msg tea.WindowSizeMsg) tea.Cmd {
	m.height = msg.Height
	m.width = msg.Width
	return nil
}

// doWin is called when the user has guessed the word correctly.
func (m *model) doWin() tea.Cmd {
	m.gameOver = true
	return tea.Sequentially(
		m.withDb(func(db *db) {
			db.addWin(m.gridRow)
			m.score = db.score()
		}),
		m.setStatus("You win!", 0),
	)
}

// doLoss is called when the user has used up all their guesses.
func (m *model) doLoss() tea.Cmd {
	m.gameOver = true
	msg := fmt.Sprintf("The word was %s. Better luck next time!", string(m.word[:]))
	return tea.Sequentially(
		m.withDb(func(db *db) {
			db.addLoss()
			m.score = db.score()
		}),
		m.setStatus(msg, 0),
	)
}

// viewStatus renders the status line.
func (m *model) viewStatus() string {
	return lipgloss.NewStyle().Foreground(colorPrimary).Render(m.status)
}

// viewGrid renders the grid.
func (m *model) viewGrid() string {
	var rows [numGuesses]string
	for i := 0; i < numGuesses; i++ {
		if i < m.gridRow {
			rows[i] = m.viewGridRowFilled(m.grid[i])
		} else if i == m.gridRow && !m.gameOver {
			rows[i] = m.viewGridRowCurrent(m.grid[i], m.gridCol)
		} else {
			rows[i] = m.viewGridRowEmpty()
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows[:]...)
}

// viewGridRowFilled renders a filled-in grid row. It chooses the appropriate
// color for each key.
func (m *model) viewGridRowFilled(word [numChars]byte) string {
	var keyStates [numChars]keyState
	letters := m.word

	// Mark keyStatusAbsent.
	for i := 0; i < numChars; i++ {
		keyStates[i] = keyStateAbsent
	}

	// Mark keyStatusCorrect.
	for i := 0; i < numChars; i++ {
		if word[i] == m.word[i] {
			keyStates[i] = keyStateCorrect
			letters[i] = 0
		}
	}

	// Mark keyStatusPresent.
	for i := 0; i < numChars; i++ {
		if keyStates[i] == keyStateCorrect {
			continue
		}
		if foundIdx := bytes.IndexByte(letters[:], word[i]); foundIdx != -1 {
			keyStates[i] = keyStatePresent
			letters[foundIdx] = 0
		}
	}

	// Render keys.
	var keys [numChars]string
	for i := 0; i < numChars; i++ {
		keys[i] = m.viewKey(string(word[i]), keyStates[i].color())
	}
	return lipgloss.JoinHorizontal(lipgloss.Bottom, keys[:]...)
}

// viewGridRowCurrent renders the current grid row. It renders an "_" character
// for the letter being currently input.
func (m *model) viewGridRowCurrent(row [numChars]byte, rowIdx int) string {
	var keys [numChars]string
	for i := 0; i < numChars; i++ {
		var key string
		if i < rowIdx {
			key = string(row[i])
		} else if i == rowIdx {
			key = "_"
		} else {
			key = " "
		}
		keys[i] = m.viewKey(key, colorPrimary)
	}
	return lipgloss.JoinHorizontal(lipgloss.Bottom, keys[:]...)
}

// viewGridRowEmpty renders an empty grid row. If the grid is locked, the keys
// are grayed out.
func (m *model) viewGridRowEmpty() string {
	keyState := keyStateUnselected
	if m.gameOver {
		keyState = keyStateAbsent
	}
	key := m.viewKey(" ", keyState.color())
	keys := [numChars]string{key, key, key, key, key}
	return lipgloss.JoinHorizontal(lipgloss.Bottom, keys[:]...)
}

// viewKeyboard renders the entire keyboard, including a border. It chooses the
// appropriate color for keys that have been guessed before.
func (m *model) viewKeyboard() string {
	topRow := m.viewKeyboardRow([]string{"Q", "W", "E", "R", "T", "Y", "U", "I", "O", "P"})
	midRow := m.viewKeyboardRow([]string{"A", "S", "D", "F", "G", "H", "J", "K", "L"})
	botRow := m.viewKeyboardRow([]string{"ENTER", "Z", "X", "C", "V", "B", "N", "M", "DELETE"})
	keys := lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.NewStyle().Padding(0, 2).Render(topRow),
		lipgloss.NewStyle().Padding(0, 4).Render(midRow),
		botRow,
	)
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(keyStateUnselected.color()).
		Padding(0, 1).
		Render(keys)
}

// viewKeyboardRow renders a single row of the keyboard. It chooses the
// appropriate color for keys that have been guessed before.
func (m *model) viewKeyboardRow(keys []string) string {
	keysRendered := make([]string, len(keys))
	for _, key := range keys {
		status := keyStateUnselected
		if len(key) == 1 {
			key := key[0]
			status = m.keyStates[key]
		}
		keysRendered = append(keysRendered, m.viewKey(key, status.color()))
	}
	return lipgloss.JoinHorizontal(lipgloss.Bottom, keysRendered...)
}

// viewKey renders a key with the given name and color.
func (*model) viewKey(key string, color lipgloss.TerminalColor) string {
	return lipgloss.NewStyle().
		Padding(0, 1).
		Border(lipgloss.NormalBorder()).
		BorderForeground(color).
		Foreground(color).
		Render(key)
}

// withDb runs a function in the context of the database. The database is
// automatically saved at the end.
func (m *model) withDb(f func(db *db)) tea.Cmd {
	db, err := loadDb()
	if err != nil {
		return m.reportError(err, "Error loading database.")
	}
	f(db)
	if err := db.save(); err != nil {
		return m.reportError(err, "Error saving database.")
	}
	return nil
}

// reportError stores the given error and prints a message to the status line.
func (m *model) reportError(err error, msg string) tea.Cmd {
	m.errors = append(m.errors, err)
	return m.setStatus(msg, 3*time.Second)
}

// msgResetStatus is sent when the status line should be reset.
type msgResetStatus struct{}

const (
	colorPrimary   = lipgloss.Color("#d7dadc")
	colorSecondary = lipgloss.Color("#626262")
	colorSeparator = lipgloss.Color("#9c9c9c")
	colorYellow    = lipgloss.Color("#b59f3b")
	colorGreen     = lipgloss.Color("#538d4e")
)

// keyState represents the state of a key.
type keyState int

const (
	keyStateUnselected keyState = iota
	keyStateAbsent
	keyStatePresent
	keyStateCorrect
)

// color returns the appropriate dark mode color for the given key state.
func (s keyState) color() lipgloss.Color {
	switch s {
	case keyStateUnselected:
		return colorPrimary
	case keyStateAbsent:
		return colorSecondary
	case keyStatePresent:
		return colorYellow
	case keyStateCorrect:
		return colorGreen
	default:
		panic("invalid key status")
	}
}

var _controls = fmt.Sprintf("%s %s %s %s %s",
	lipgloss.NewStyle().Foreground(colorPrimary).Render("ctrl+c"),
	lipgloss.NewStyle().Foreground(colorSecondary).Render("quit"),
	lipgloss.NewStyle().Foreground(colorSeparator).Render("//"),
	lipgloss.NewStyle().Foreground(colorPrimary).Render("ctrl+r"),
	lipgloss.NewStyle().Foreground(colorSecondary).Render("restart"),
)

// isAsciiUpper checks if a rune is between A-Z.
func isAsciiUpper(r rune) bool {
	return 'A' <= r && r <= 'Z'
}

// toAsciiUpper converts a rune to uppercase if it is between A-Z.
func toAsciiUpper(r rune) rune {
	if 'a' <= r && r <= 'z' {
		r -= 'a' - 'A'
	}
	return r
}

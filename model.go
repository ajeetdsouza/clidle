package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"time"

	"log/slog"

	"github.com/ajeetdsouza/clidle/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	// _numGuesses is the maximum number of guesses you can make.
	_numGuesses = 6
	// _numChars is the word size in characters.
	_numChars = 5
)

type model struct {
	ctx        context.Context
	store      *store.Queries
	dictionary Dictionary

	gameID   int
	gameOver bool

	score  int
	answer [_numChars]byte

	status        string
	statusPending int

	windowHeight int
	windowWidth  int

	grid      [_numGuesses][_numChars]byte
	gridRow   int
	gridCol   int
	keyStates map[byte]keyState
}

var _ tea.Model = (*model)(nil)

func newModel(ctx context.Context, store *store.Queries, dictionary Dictionary) *model {
	return &model{
		ctx:        ctx,
		store:      store,
		dictionary: dictionary,
		keyStates:  make(map[byte]keyState, 26),
	}
}

// Init is the first function that is called when the UI is created.
func (m *model) Init() tea.Cmd {
	m.doRestart()
	return nil
}

// Update is called when a message is received. It inspects messages and, in response,
// updates the Model and sends a command.
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
			m.doRestart()
			return m, nil
		case tea.KeyBackspace:
			return m, m.doDeleteChar()
		case tea.KeyEnter:
			if m.gameOver {
				m.doRestart()
				return m, nil
			}
			return m, m.doAcceptGuess()
		case tea.KeyRunes:
			if len(msg.Runes) == 1 {
				return m, m.doAcceptChar(msg.Runes[0])
			}
		}
	case tea.WindowSizeMsg:
		// If the window is resized, store its new dimensions.
		return m, m.doResize(msg)
	}
	return m, nil
}

func (m *model) View() string {
	status := m.viewStatus()
	grid := m.viewGrid()
	keyboard := m.viewKeyboard()

	// Truncate the status if it is too long.
	if len(status) > m.windowWidth && m.windowWidth > 3 {
		status = status[:m.windowWidth-3] + "..."
	}

	// Drop the keyboard if it doesn't fit.
	height := lipgloss.Height(status) + lipgloss.Height(grid) + lipgloss.Height(keyboard)
	width := lipgloss.Width(keyboard)
	if width < lipgloss.Width(status) || width < lipgloss.Width(grid) {
		width = 0
	}
	if m.windowHeight < height || m.windowWidth < width {
		keyboard = ""
	}

	game := lipgloss.JoinVertical(lipgloss.Center, status, grid, keyboard, _controls)
	return lipgloss.Place(m.windowWidth, m.windowHeight, lipgloss.Center, lipgloss.Center, game)
}

// doAcceptGuess accepts the current word.
func (m *model) doAcceptGuess() tea.Cmd {
	if m.gameOver {
		return nil
	}

	// Only accept a word if it is complete.
	if m.gridCol != _numChars {
		return m.setStatus("Your guess must be a 5-letter word.", 1*time.Second)
	}

	// Check if the input guess is valid.
	guess := m.grid[m.gridRow]
	if !m.dictionary.IsWord(string(guess[:])) {
		return m.setStatus("That's not a valid word.", 1*time.Second)
	}

	// Save the guess.
	if err := m.saveGuess(string(guess[:])); err != nil {
		slog.Error("error saving guess", slog.Any("error", err))
	}

	// Update the state of the used letters.
	success := true
	for idx, key := range guess {
		keyState := _keyStateAbsent
		if key == m.answer[idx] {
			keyState = _keyStateCorrect
		} else {
			success = false
			if bytes.IndexByte(m.answer[:], key) != -1 {
				keyState = _keyStatePresent
			}
		}
		m.keyStates[key] = max(keyState, m.keyStates[key])
	}

	// Move the cursor to the next row.
	m.gridRow++
	m.gridCol = 0

	// Check if the game is over.
	if success {
		return m.doWin()
	} else if m.gridRow == _numGuesses {
		return m.doLoss()
	}

	return nil
}

func (m *model) saveGuess(guess string) error {
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
	defer cancel()

	// Create a new game if one doesn't exist.
	if m.gameID == 0 {
		answer := sql.NullString{String: string(m.answer[:]), Valid: true}
		game, err := m.store.CreateGame(ctx, answer)
		if err != nil {
			return err
		}
		m.gameID = int(game.ID)
	}

	params := store.CreateGuessParams{
		GameID: sql.NullInt64{Int64: int64(m.gameID), Valid: true},
		Guess:  sql.NullString{String: guess, Valid: true},
	}
	_, err := m.store.CreateGuess(ctx, params)
	if err != nil {
		return err
	}

	return nil
}

// doAcceptChar adds one input character to the current word.
func (m *model) doAcceptChar(ch rune) tea.Cmd {
	// Only accept a character if the current word is incomplete.
	if m.gameOver || !(m.gridRow < _numGuesses && m.gridCol < _numChars) {
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
	m.windowHeight = msg.Height
	m.windowWidth = msg.Width
	return nil
}

// doWin is called when the user has guessed the word correctly.
func (m *model) doWin() tea.Cmd {
	m.gameOver = true
	m.updateScore()
	return m.setStatus("You win!", 0)
}

// doLoss is called when the user has used up all their guesses.
func (m *model) doLoss() tea.Cmd {
	m.gameOver = true
	m.updateScore()
	msg := fmt.Sprintf("The word was %s. Better luck next time!", string(m.answer[:]))
	return m.setStatus(msg, 0)
}

// doRestart resets the game state and starts a new game.
func (m *model) doRestart() {
	// Start a new game.
	m.gameID = 0
	m.gameOver = false

	// Set the puzzle answer.
	answer := m.dictionary.GetRandomCommonWord()
	copy(m.answer[:], answer)

	// Reset the grid.
	m.gridCol = 0
	m.gridRow = 0

	// Clear the key state.
	for k := range m.keyStates {
		delete(m.keyStates, k)
	}

	// Reset the status message.
	m.updateScore()
	m.resetStatus()
}

// updateScore fetches the current total score from the database.
func (m *model) updateScore() {
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
	defer cancel()

	score, err := m.store.GetTotalScore(ctx)
	if err != nil {
		slog.Error("error fetching score", slog.Any("error", err))
		return
	}
	m.score = int(score.Float64)
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

// viewStatus renders the status line.
func (m *model) viewStatus() string {
	return lipgloss.NewStyle().Foreground(_colorPrimary).Render(m.status)
}

// viewGrid renders the grid.
func (m *model) viewGrid() string {
	var rows [_numGuesses]string
	for i := 0; i < _numGuesses; i++ {
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
func (m *model) viewGridRowFilled(word [_numChars]byte) string {
	var keyStates [_numChars]keyState
	letters := m.answer

	// Mark keyStatusAbsent.
	for i := 0; i < _numChars; i++ {
		keyStates[i] = _keyStateAbsent
	}

	// Mark keyStatusCorrect.
	for i := 0; i < _numChars; i++ {
		if word[i] == m.answer[i] {
			keyStates[i] = _keyStateCorrect
			letters[i] = 0
		}
	}

	// Mark keyStatusPresent.
	for i := 0; i < _numChars; i++ {
		if keyStates[i] == _keyStateCorrect {
			continue
		}
		if foundIdx := bytes.IndexByte(letters[:], word[i]); foundIdx != -1 {
			keyStates[i] = _keyStatePresent
			letters[foundIdx] = 0
		}
	}

	// Render keys.
	var keys [_numChars]string
	for i := 0; i < _numChars; i++ {
		keys[i] = m.viewKey(string(word[i]), keyStates[i].color())
	}
	return lipgloss.JoinHorizontal(lipgloss.Bottom, keys[:]...)
}

// viewGridRowCurrent renders the current grid row. It renders an "_" character
// for the letter being currently input.
func (m *model) viewGridRowCurrent(row [_numChars]byte, rowIdx int) string {
	var keys [_numChars]string
	for i := 0; i < _numChars; i++ {
		var key string
		if i < rowIdx {
			key = string(row[i])
		} else if i == rowIdx {
			key = "_"
		} else {
			key = " "
		}
		keys[i] = m.viewKey(key, _colorPrimary)
	}
	return lipgloss.JoinHorizontal(lipgloss.Bottom, keys[:]...)
}

// viewGridRowEmpty renders an empty grid row. If the grid is locked, the keys
// are grayed out.
func (m *model) viewGridRowEmpty() string {
	keyState := _keyStateUnselected
	if m.gameOver {
		keyState = _keyStateAbsent
	}
	key := m.viewKey(" ", keyState.color())
	keys := [_numChars]string{key, key, key, key, key}
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
		BorderForeground(_keyStateUnselected.color()).
		Padding(0, 1).
		Render(keys)
}

// viewKeyboardRow renders a single row of the keyboard. It chooses the
// appropriate color for keys that have been guessed before.
func (m *model) viewKeyboardRow(keys []string) string {
	keysRendered := make([]string, len(keys))
	for _, key := range keys {
		status := _keyStateUnselected
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

// msgResetStatus is sent when the status line should be reset.
type msgResetStatus struct{}

const (
	_colorPrimary   = lipgloss.Color("#d7dadc")
	_colorSecondary = lipgloss.Color("#626262")
	_colorSeparator = lipgloss.Color("#9c9c9c")
	_colorYellow    = lipgloss.Color("#b59f3b")
	_colorGreen     = lipgloss.Color("#538d4e")
)

// keyState represents the state of a key.
type keyState int

const (
	_keyStateUnselected keyState = iota
	_keyStateAbsent
	_keyStatePresent
	_keyStateCorrect
)

// color returns the appropriate dark mode color for the given key state.
func (s keyState) color() lipgloss.Color {
	switch s {
	case _keyStateUnselected:
		return _colorPrimary
	case _keyStateAbsent:
		return _colorSecondary
	case _keyStatePresent:
		return _colorYellow
	case _keyStateCorrect:
		return _colorGreen
	default:
		panic("invalid key status")
	}
}

var _controls = fmt.Sprintf("%s %s %s %s %s",
	lipgloss.NewStyle().Foreground(_colorPrimary).Render("ctrl+c"),
	lipgloss.NewStyle().Foreground(_colorSecondary).Render("quit"),
	lipgloss.NewStyle().Foreground(_colorSeparator).Render("//"),
	lipgloss.NewStyle().Foreground(_colorPrimary).Render("ctrl+r"),
	lipgloss.NewStyle().Foreground(_colorSecondary).Render("restart"),
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

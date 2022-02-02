package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/renameio/maybe"
	"github.com/pkg/errors"
)

// db is the file where the game statistics are stored.
type db struct {
	// Guesses stores the win statistics of each game. Guesses[0] is the number
	// of games that were lost, Guesses[1] is the number of games that were won
	// with 1 guess, etc.
	Guesses [numGuesses + 1]int `json:"guesses"`
}

// loadDb reads the database from dbPath.
func loadDb() (*db, error) {
	file, err := os.Open(pathDb)
	if err != nil {
		if os.IsNotExist(err) {
			return &db{}, nil
		}
		return nil, errors.Wrap(err, "could not find database")
	}
	var db db
	if err := json.NewDecoder(file).Decode(&db); err != nil {
		return nil, errors.Wrap(err, "could not read from database")
	}
	return &db, nil
}

// save atomically (best effort) writes the database to dbPath.
func (db *db) save() error {
	dir := filepath.Dir(pathDb)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return errors.Wrapf(err, "could not create data directory: %s", dir)
	}
	data, err := json.MarshalIndent(&db, "", "  ")
	if err != nil {
		return errors.Wrap(err, "could not serialize database")
	}
	if err := maybe.WriteFile(pathDb, data, 0644); err != nil {
		return errors.Wrapf(err, "could not write to database: %s", pathDb)
	}
	return nil
}

// addWin adds a win to the game statistics. It also stores the number of
// guesses it took, in the range [1,numGuesses].
func (db *db) addWin(guesses int) {
	if !(1 <= guesses && guesses <= numGuesses) {
		panic(fmt.Sprintf("guesses out of range: %d", guesses))
	}
	db.Guesses[guesses]++
}

// addLoss adds a loss to the game statistics.
func (db *db) addLoss() {
	db.Guesses[0]++
}

// score returns the current total score based on the statistics.
//
// A game that was lost provides 0 points. A game that was won provides 50
// points, plus an extra 10 points for each remaining guess.
func (db *db) score() int {
	score := 0
	for i, guess := range db.Guesses[1:] {
		score += (100 - 10*i) * guess
	}
	return score
}

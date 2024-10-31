-- name: CreateGame :one
INSERT INTO game (answer)
VALUES (?)
RETURNING *;

-- name: CreateGuess :one
INSERT INTO guess (game_id, guess)
VALUES (?, ?)
RETURNING *;

-- name: GetTotalScore :one
WITH game_scores AS (
    SELECT game.id, (10 * (11 - COUNT(guess.id))) AS score
    FROM game
    INNER JOIN guess victory ON game.id = victory.game_id AND game.answer = victory.guess
    INNER JOIN guess ON game.id = guess.game_id
    GROUP BY game.id
)
SELECT SUM(score) FROM game_scores;

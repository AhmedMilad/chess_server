package utils

import (
	"chess_server/database"
	"chess_server/models"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-redis/redis/v8"
	"github.com/gorilla/websocket"
)

var Ctx context.Context
var Board [8][8]string = [8][8]string{
	{"r", "n", "b", "q", "k", "b", "n", "r"},
	{"p", "p", "p", "p", "p", "p", "p", "p"},
	{" ", " ", " ", " ", " ", " ", " ", " "},
	{" ", " ", " ", " ", " ", " ", " ", " "},
	{" ", " ", " ", " ", " ", " ", " ", " "},
	{" ", " ", " ", " ", " ", " ", " ", " "},
	{"P", "P", "P", "P", "P", "P", "P", "P"},
	{"R", "N", "B", "Q", "K", "B", "N", "R"},
}

type NotificationMessage struct {
	Type     string `json:"type"`
	GameId   int    `json:"game_id"`
	Opponent Player `json:"opponent"`
	IsBlack  bool   `json:"is_black"`
	Board    string `json:"board"`
	Turn     int    `json:"turn"`
}

func InitGame() {
	Ctx = context.Background()
}

func createGame(p1, p2 Player) {
	boardNotation, _ := GetFenNotation(Board)
	turn := rand.Intn(2)
	game := models.Game{
		Player1ID:  p1.UserID,
		Player2ID:  p2.UserID,
		GameTypeID: p1.GameTypeID,
		Status:     "ongoing",
		Board:      *boardNotation,
		PlayerTurn: 1,
	}
	if turn == 1 {
		temp := game.Player1ID
		game.Player1ID = game.Player2ID
		game.Player2ID = temp
		tempPlayer := p1
		p1 = p2
		p2 = tempPlayer
	}
	db.DB.Create(&game)
	startGame := "start_game"
	boardNotation, err := GetFenNotation(Board)
	if err != nil {
		Players[p1.UserID].Close()
		delete(Players, p1.UserID)
	}
	player1Notification := NotificationMessage{
		Type:     startGame,
		GameId:   int(game.ID),
		Opponent: p2,
		IsBlack:  false,
		Board:    *boardNotation,
		Turn:     game.PlayerTurn,
	}
	player2Notification := NotificationMessage{
		Type:     startGame,
		GameId:   int(game.ID),
		Opponent: p1,
		IsBlack:  true,
		Board:    *boardNotation,
		Turn:     game.PlayerTurn,
	}
	player1Data, _ := json.Marshal(&player1Notification)
	player2Data, _ := json.Marshal(&player2Notification)

	if err := Players[p1.UserID].WriteMessage(websocket.TextMessage, []byte(player1Data)); err != nil {
		Players[p1.UserID].Close()
		delete(Players, p1.UserID)
	}
	if err := Players[p2.UserID].WriteMessage(websocket.TextMessage, []byte(player2Data)); err != nil {
		Players[p1.UserID].Close()
		delete(Players, p1.UserID)
	}

	fmt.Printf("Game created: %d vs %d\n", p1.UserID, p2.UserID)
}

type Player struct {
	UserID               uint
	GameTypeID           uint
	Rating               int
	LowerBoundRatingDiff int
	UpperBoundRatingDiff int
}

func mutualFit(p1, p2 Player) bool {
	return p2.Rating >= p1.Rating-p1.LowerBoundRatingDiff &&
		p2.Rating <= p1.Rating+p1.UpperBoundRatingDiff &&
		p1.Rating >= p2.Rating-p2.LowerBoundRatingDiff &&
		p1.Rating <= p2.Rating+p2.UpperBoundRatingDiff
}

func EnqueuePlayer(userId uint, gameTypeId int) {
	var user models.User
	db.DB.Preload("Ratings.GameType").Preload("Setting").First(&user, userId)

	var playerRating int
	for _, rating := range user.Ratings {
		if rating.GameTypeID == uint(gameTypeId) {
			playerRating = rating.Rating
		}
	}

	player := Player{
		UserID:               user.ID,
		GameTypeID:           uint(gameTypeId),
		Rating:               playerRating,
		LowerBoundRatingDiff: int(user.Setting.LowerBoundPlayerRatingDiff),
		UpperBoundRatingDiff: int(user.Setting.UpperBoundPlayerRatingDiff),
	}

	serialized, err := json.Marshal(player)
	if err != nil {
		fmt.Println("Error marshaling player:", err)
		return
	}
	serializedStr := string(serialized)

	exists, err := RDB.SIsMember(Ctx, "players_q_set", serializedStr).Result()
	if err != nil {
		fmt.Println("Error checking set:", err)
		return
	}

	if exists {
		fmt.Println("Player already in queue, skipping")
		return
	}

	pipe := RDB.TxPipeline()
	pipe.SAdd(Ctx, "players_q_set", serializedStr)
	pipe.RPush(Ctx, "players_q", serializedStr)
	_, err = pipe.Exec(Ctx)
	if err != nil {
		fmt.Println("Error enqueuing player:", err)
		return
	}

	fmt.Println("Player enqueued")
}

func MatchmakingWorker() {
	for {
		players, err := RDB.LRange(Ctx, "players_q", 0, -1).Result()
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		if len(players) == 0 {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		for _, playerRaw := range players {
			matched := MatchPlayer(playerRaw)

			if matched {
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func MatchPlayer(playerRaw string) bool {
	err := RDB.Watch(Ctx, func(tx *redis.Tx) error {
		players, err := tx.LRange(Ctx, "players_q", 0, -1).Result()
		if err != nil {
			return err
		}

		var p Player
		if err := json.Unmarshal([]byte(playerRaw), &p); err != nil {
			return err
		}

		for _, raw := range players {
			var candidate Player
			if err := json.Unmarshal([]byte(raw), &candidate); err != nil {
				continue
			}

			if candidate.UserID == p.UserID {
				continue
			}
			if candidate.GameTypeID == p.GameTypeID && mutualFit(p, candidate) {
				pipe := tx.TxPipeline()
				pipe.LRem(Ctx, "players_q", 1, raw)
				pipe.LRem(Ctx, "players_q", 1, playerRaw)
				pipe.SRem(Ctx, "players_q_set", raw)
				pipe.SRem(Ctx, "players_q_set", playerRaw)
				_, err := pipe.Exec(Ctx)
				if err != nil {
					return err
				}

				fmt.Printf("Matched Player %d with Player %d in gameType %d\n",
					p.UserID, candidate.UserID, p.GameTypeID)

				/*
				* create game in DB
				* TODO: notify players
				 */
				createGame(p, candidate)

				return nil
			}
		}

		return nil
	}, "players_q")

	if err != nil {
		fmt.Println("Matchmaking transaction failed:", err)
		return false
	}

	return true
}

func GetFenNotation(board [8][8]string) (*string, error) {
	fen := ""
	if len(board) != 8 {
		return nil, fmt.Errorf("invalid board: expected 8 rows, got %d", len(board))
	}
	for i, row := range board {
		if len(row) != 8 {
			return nil, fmt.Errorf("invalid board: expected 8 rows, got %d", len(board))
		}
		cnt := 0
		for _, elem := range row {
			if elem == "" || elem == " " {
				cnt++
				continue
			}
			if !slices.Contains([]string{"p", "r", "n", "b", "q", "k"}, strings.ToLower(elem)) {
				return nil, fmt.Errorf("invalid board piece name")
			}
			var emptySquares = ""
			if cnt > 0 {
				emptySquares = strconv.Itoa(cnt)
				cnt = 0
			}
			fen = fen + emptySquares + elem
		}
		if cnt > 0 {
			fen = fen + strconv.Itoa(cnt)
		}
		if i < 7 {
			fen = fen + "/"
		}
	}
	return &fen, nil
}

func GetBoardFromFenNotation(fenNotation string) (*[8][8]string, error) {
	var board [8][8]string
	row, col := 0, 0
	validPieces := []string{"p", "r", "n", "b", "q", "k", "P", "R", "N", "B", "Q", "K"}
	for _, elem := range fenNotation {
		if unicode.IsDigit(elem) {
			count := int(elem - '0')
			for i := 0; i < count; i++ {
				if row >= 8 || col >= 8 {
					return nil, fmt.Errorf("too many squares")
				}
				board[row][col] = " "
				col++
			}
		} else if elem == '/' {
			if col != 8 {
				return nil, fmt.Errorf("row %d has %d columns, expected 8", row+1, col)
			}
			row++
			col = 0
			if row >= 8 {
				return nil, fmt.Errorf("too many rows")
			}
		} else {
			if !slices.Contains(validPieces, string(elem)) {
				return nil, fmt.Errorf("invalid piece: %s", string(elem))
			}
			if row >= 8 || col >= 8 {
				return nil, fmt.Errorf("too many squares")
			}
			board[row][col] = string(elem)
			col++
		}
	}
	if row != 7 || col != 8 {
		return nil, fmt.Errorf("incomplete board: ended at row %d, column %d", row+1, col)
	}
	return &board, nil
}

func getMoveNotationIndex(moveNotation string) (*[2]int, error) {
	if len(moveNotation) < 2 {
		return nil, fmt.Errorf("invalid move notation: %s", moveNotation)
	}

	square := moveNotation[len(moveNotation)-2:]
	file := square[0]
	rank := square[1]

	if !unicode.IsLetter(rune(file)) || !unicode.IsDigit(rune(rank)) {
		return nil, fmt.Errorf("invalid square: %s", square)
	}

	col := int(unicode.ToLower(rune(file)) - 'a')
	rowDigit := int(rank - '0')
	if rowDigit < 1 || rowDigit > 8 {
		return nil, fmt.Errorf("invalid rank: %d", rowDigit)
	}
	row := 8 - rowDigit

	return &[2]int{row, col}, nil
}

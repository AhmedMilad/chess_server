package utils

import (
	"chess_server/database"
	"chess_server/models"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

func handleMove(game models.Game, message *Message) error {
	var moveData struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.Unmarshal(message.Data, &moveData); err != nil {
		return errors.New("Error unmarshaling move data")
	}

	var moves []string
	if len(game.Moves) > 0 {
		if err := json.Unmarshal(game.Moves, &moves); err != nil {
			fmt.Println("Failed to unmarshal moves:", err)
			moves = []string{}
		}
	}
	moves = append(moves, moveData.To)
	newMoves, _ := json.Marshal(moves)
	game.Moves = newMoves
	board, err := GetBoardFromFenNotation(game.Board)

	if err != nil {
		return errors.New("Invalid board notation")
	}

	fromIndex, err := getMoveNotationIndex(moveData.From)
	if err != nil {
		return errors.New("Invalid move from")
	}

	toIndex, err := getMoveNotationIndex(moveData.To)
	if err != nil {
		return errors.New("Invalid move to")
	}

	currentSquare := board[(*fromIndex)[0]][(*fromIndex)[1]]
	board[(*fromIndex)[0]][(*fromIndex)[1]] = " "
	board[(*toIndex)[0]][(*toIndex)[1]] = currentSquare

	newBoardNotation, err := GetFenNotation(*board)
	if err != nil {
		return errors.New("Invalid board notation")
	}

	game.Board = *newBoardNotation
	if game.PlayerTurn == 1 {
		game.PlayerTurn = 2
	} else {
		game.PlayerTurn = 1
	}
	if err := db.DB.Save(&game).Error; err != nil {
		fmt.Println("Failed to save move:", err)
	}
	message.Board = *newBoardNotation
	message.Turn = game.PlayerTurn

	return nil
}

func handleKingSideCastle(game models.Game, pieceColor string, message *Message) error {

	board, err := GetBoardFromFenNotation(game.Board)

	if err != nil {
		return errors.New("Invalid board notation")
	}

	if pieceColor == "w" {
		if board[7][4] == "K" && board[7][7] == "R" { //TODO validate if any is moved before
			ok := true
			for st := 4; st <= 7; st++ {
				ok = ok && getDiagonalThreat(7, st, pieceColor, *board)
				ok = ok && getAntiDiagonalThreat(7, st, pieceColor, *board)
				ok = ok && getFileThreat(7, st, pieceColor, *board)
				ok = ok && getRowThreat(7, st, pieceColor, *board)
				ok = ok && getKnightThreat(7, st, pieceColor, *board)
				ok = ok && getPawnThreat(7, st, pieceColor, *board)
				ok = ok && getKingThreat(7, st, pieceColor, *board)
			}

			if !ok {
				return errors.New("Invalid Move")
			}
			swap(7, 4, 7, 6, board)
			swap(7, 7, 7, 5, board)
		}
	} else {
		if board[0][4] == "k" && board[0][7] == "r" { //TODO validate if any is moved before
			ok := true
			for st := 4; st <= 7; st++ {
				ok = ok && getDiagonalThreat(0, st, pieceColor, *board)
				ok = ok && getAntiDiagonalThreat(0, st, pieceColor, *board)
				ok = ok && getFileThreat(0, st, pieceColor, *board)
				ok = ok && getRowThreat(0, st, pieceColor, *board)
				ok = ok && getKnightThreat(0, st, pieceColor, *board)
				ok = ok && getPawnThreat(0, st, pieceColor, *board)
				ok = ok && getKingThreat(0, st, pieceColor, *board)
			}

			if !ok {
				return errors.New("Invalid Move")
			}
			swap(0, 4, 0, 6, board)
			swap(0, 7, 0, 5, board)
		}
	}

	newBoardNotation, err := GetFenNotation(*board)
	if err != nil {
		return errors.New("Invalid board notation")
	}

	game.Board = *newBoardNotation
	if game.PlayerTurn == 1 {
		game.PlayerTurn = 2
	} else {
		game.PlayerTurn = 1
	}
	if err := db.DB.Save(&game).Error; err != nil {
		fmt.Println("Failed to save move:", err)
	}
	message.Board = *newBoardNotation
	message.Turn = game.PlayerTurn

	return nil
}

func handleLongCastle(game models.Game, pieceColor string, message *Message) error {
	board, err := GetBoardFromFenNotation(game.Board)

	if err != nil {
		return errors.New("Invalid board notation")
	}

	if pieceColor == "w" {
		if board[7][4] == "K" && board[7][0] == "R" { //TODO validate if any is moved before
			ok := true
			for st := 4; st >= 0; st-- {
				ok = ok && getDiagonalThreat(7, st, pieceColor, *board)
				ok = ok && getAntiDiagonalThreat(7, st, pieceColor, *board)
				ok = ok && getFileThreat(7, st, pieceColor, *board)
				ok = ok && getRowThreat(7, st, pieceColor, *board)
				ok = ok && getKnightThreat(7, st, pieceColor, *board)
				ok = ok && getPawnThreat(7, st, pieceColor, *board)
				ok = ok && getKingThreat(7, st, pieceColor, *board)
			}

			if !ok {
				return errors.New("Invalid Move")
			}
			swap(7, 4, 7, 2, board)
			swap(7, 0, 7, 3, board)
		}
	} else {
		if board[0][4] == "k" && board[0][7] == "r" { //TODO validate if any is moved before
			ok := true
			for st := 4; st >= 0; st-- {
				ok = ok && getDiagonalThreat(0, st, pieceColor, *board)
				ok = ok && getAntiDiagonalThreat(0, st, pieceColor, *board)
				ok = ok && getFileThreat(0, st, pieceColor, *board)
				ok = ok && getRowThreat(0, st, pieceColor, *board)
				ok = ok && getKnightThreat(0, st, pieceColor, *board)
				ok = ok && getPawnThreat(0, st, pieceColor, *board)
				ok = ok && getKingThreat(0, st, pieceColor, *board)
			}

			if !ok {
				return errors.New("Invalid Move")
			}
			swap(0, 4, 0, 2, board)
			swap(0, 0, 0, 3, board)
		}
	}

	newBoardNotation, err := GetFenNotation(*board)
	if err != nil {
		return errors.New("Invalid board notation")
	}

	game.Board = *newBoardNotation
	if game.PlayerTurn == 1 {
		game.PlayerTurn = 2
	} else {
		game.PlayerTurn = 1
	}
	if err := db.DB.Save(&game).Error; err != nil {
		fmt.Println("Failed to save move:", err)
	}
	message.Board = *newBoardNotation
	message.Turn = game.PlayerTurn

	return nil
}

func handleEnpassent(game models.Game, pieceColor string, message *Message) error {
	return nil
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

func getDiagonalThreat(stRow int, stCol int, color string, board [8][8]string) bool {
	targetPieces := []string{"b", "q"}
	if color == "b" {
		targetPieces = []string{"B", "Q"}
	}

	curStRow := stRow
	curStCol := stCol

	for math.Max(float64(curStRow), float64(curStCol)) <= 7 {
		boardPiece := board[curStRow][curStCol]
		if boardPiece != "" {
			if slices.Contains(targetPieces, boardPiece) {
				return false
			} else {
				return true
			}
		}
		curStCol++
		curStRow++
	}

	curStRow = stRow
	curStCol = stCol

	for math.Min(float64(curStRow), float64(curStCol)) >= 0 {
		boardPiece := board[curStRow][curStCol]
		if boardPiece != "" {
			if slices.Contains(targetPieces, boardPiece) {
				return false
			} else {
				return true
			}
		}
		curStCol--
		curStRow--
	}

	return true
}

func getAntiDiagonalThreat(stRow int, stCol int, color string, board [8][8]string) bool {
	targetPieces := []string{"b", "q"}
	if color == "b" {
		targetPieces = []string{"B", "Q"}
	}

	curStRow := stRow
	curStCol := stCol

	for curStCol >= 0 && curStRow <= 7 {
		boardPiece := board[curStRow][curStCol]
		if boardPiece != "" {
			if slices.Contains(targetPieces, boardPiece) {
				return false
			} else {
				return true
			}
		}
		curStCol--
		curStRow++
	}

	curStRow = stRow
	curStCol = stCol

	for curStRow >= 0 && curStCol <= 7 {
		boardPiece := board[curStRow][curStCol]
		if boardPiece != "" {
			if slices.Contains(targetPieces, boardPiece) {
				return false
			} else {
				return true
			}
		}
		curStCol++
		curStRow--
	}

	return true
}

func getFileThreat(stRow int, stCol int, color string, board [8][8]string) bool {
	targetPieces := []string{"r", "q"}
	if color == "b" {
		targetPieces = []string{"R", "Q"}
	}

	curStRow := stRow

	for curStRow <= 7 {
		boardPiece := board[curStRow][stCol]
		if boardPiece != "" {
			if slices.Contains(targetPieces, boardPiece) {
				return false
			} else {
				return true
			}
		}
		curStRow++
	}

	curStRow = stRow

	for curStRow >= 0 {
		boardPiece := board[curStRow][stCol]
		if boardPiece != "" {
			if slices.Contains(targetPieces, boardPiece) {
				return false
			} else {
				return true
			}
		}
		curStRow--
	}

	return true
}

func getRowThreat(stRow int, stCol int, color string, board [8][8]string) bool {
	targetPieces := []string{"r", "k", "q"}
	if color == "b" {
		targetPieces = []string{"R", "K", "Q"}
	}

	curStCol := stCol

	for curStCol <= 7 {
		boardPiece := board[stRow][curStCol]
		if boardPiece != "" {
			if slices.Contains(targetPieces, boardPiece) {
				return false
			} else {
				return true
			}
		}
		curStCol++
	}

	curStCol = stCol

	for curStCol >= 0 {
		boardPiece := board[stRow][curStCol]
		if boardPiece != "" {
			if slices.Contains(targetPieces, boardPiece) {
				return false
			} else {
				return true
			}
		}
		curStCol--
	}

	return true
}

func getKnightThreat(stRow int, stCol int, color string, board [8][8]string) bool {
	targetPiece := "n"
	if color == "b" {
		targetPiece = "N"
	}
	moves := [][]int{{2, 1}, {2, -1}, {-2, 1}, {-2, -1}, {1, 2}, {1, -2}, {-1, 2}, {-1, -2}}
	for _, move := range moves {
		row := stRow + move[0]
		col := stCol + move[1]
		if math.Min(float64(row), float64(col)) < 0 || math.Max(float64(row), float64(col)) > 7 {
			continue
		}
		if board[row][col] == targetPiece {
			return false
		}
	}
	return true
}
func getPawnThreat(stRow int, stCol int, color string, board [8][8]string) bool {

	if color == "w" {
		if stRow-1 >= 0 && stCol-1 >= 0 {
			if board[stRow-1][stCol-1] == "p" {
				return false
			}
		}

		if stRow-1 >= 0 && stCol+1 <= 7 {
			if board[stRow-1][stCol+1] == "p" {
				return false
			}
		}

	} else {
		if stRow+1 <= 7 && stCol-1 >= 0 {
			if board[stRow+1][stCol-1] == "P" {
				return false
			}
		}

		if stRow+1 <= 7 && stCol+1 <= 7 {
			if board[stRow+1][stCol+1] == "P" {
				return false
			}
		}
	}

	return true
}

func getKingThreat(stRow int, stCol int, color string, board [8][8]string) bool {
	targetPiece := "k"
	if color == "b" {
		targetPiece = "K"
	}
	moves := [][]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}, {0, 1}, {0, -1}, {-1, 0}, {1, 0}}
	for _, move := range moves {
		row := stRow + move[0]
		col := stCol + move[1]
		if math.Min(float64(row), float64(col)) < 0 || math.Max(float64(row), float64(col)) > 7 {
			continue
		}
		if board[row][col] == targetPiece {
			return false
		}
	}
	return true
}

func swap(stRow int, stCol int, tRow int, tCol int, board *[8][8]string) {
	temp := board[stRow][stCol]
	board[stRow][stCol] = board[tRow][tCol]
	board[tRow][tCol] = temp
}

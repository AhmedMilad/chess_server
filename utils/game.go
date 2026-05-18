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
	"sync"
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

var mu sync.Mutex

type NotificationMessage struct {
	Type            string `json:"type"`
	GameId          int    `json:"game_id"`
	Opponent        Player `json:"opponent"`
	IsBlack         bool   `json:"is_black"`
	Board           string `json:"board"`
	Turn            int    `json:"turn"`
	EnpassantSquare string `json:"enpassant_square"`
}

func InitGame() {
	Ctx = context.Background()
}

type Player struct {
	UserID               uint
	GameTypeID           uint
	Rating               int
	LowerBoundRatingDiff int
	UpperBoundRatingDiff int
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

	gameStates := []models.GameState{
		{
			UserID: p1.UserID,
			GameID: game.ID,
		},
		{
			UserID: p2.UserID,
			GameID: game.ID,
		},
	}

	db.DB.Create(&gameStates)

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
		func() {
			mu.Lock()
			defer mu.Unlock()

			players, err := RDB.LRange(Ctx, "players_q", 0, -1).Result()
			if err != nil {
				time.Sleep(500 * time.Millisecond)
				return
			}

			if len(players) == 0 {
				time.Sleep(500 * time.Millisecond)
				return
			}

			for _, playerRaw := range players {
				matched := MatchPlayer(playerRaw)

				if matched {
					break
				}
			}
			time.Sleep(500 * time.Millisecond)
		}()
	}
}

func MatchPlayer(playerRaw string) bool {
	//TODO this piece of code needs to be revised if there is a faster way to get the match.

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

func GenericHandleMove(game models.Game, message *Message) error {
	var moveData struct {
		From string `json:"from"`
		To   string `json:"to"`
	}

	if err := json.Unmarshal(message.Data, &moveData); err != nil {
		return errors.New("Error unmarshaling move data")
	}

	board, err := GetBoardFromFenNotation(game.Board)

	if err != nil {
		return err
	}

	fromIndex, err := getMoveNotationIndex(moveData.From)
	if err != nil {
		return errors.New("Invalid move to")
	}

	toIndex, err := getMoveNotationIndex(moveData.To)
	if err != nil {
		return errors.New("Invalid move to")
	}

	fromY := fromIndex[0]
	fromX := fromIndex[1]

	toY := toIndex[0]
	toX := toIndex[1]

	piece := board[fromY][fromX]

	if piece == " " {
		return errors.New("Invalid piece")
	}

	targetPlayerID := game.Player1ID
	opponentPlayerID := game.Player2ID

	if game.PlayerTurn == 2 {
		opponentPlayerID = game.Player1ID
		targetPlayerID = game.Player2ID
	}

	// TODO fetch game states in one query

	playerGameState := models.GameState{}
	opponentGameState := models.GameState{}

	err = db.DB.Where("game_id = ? AND user_id = ?", game.ID, targetPlayerID).First(&playerGameState).Error

	if err != nil {
		return err
	}

	err = db.DB.Where("game_id = ? AND user_id = ?", game.ID, opponentPlayerID).First(&opponentGameState).Error

	if err != nil {
		return err
	}

	// player turn = 1, means white to play and 2 for black
	sqr := board[fromY][fromX]

	if sqr == " " || (game.PlayerTurn == 1) != (strings.ToUpper(sqr) == sqr) {

		return errors.New("Invalid piece")
	}

	switch piece {
	case "p":

		err := movePawn(false, fromX, fromY, toX, toY, &playerGameState, &opponentGameState, board)

		if err != nil {
			return err
		}

	case "q":
		err := moveQueen(false, fromX, fromY, toX, toY, &playerGameState, board)

		if err != nil {
			return err
		}

	case "b":
		err := moveBishop(false, fromX, fromY, toX, toY, &playerGameState, board)

		if err != nil {
			return err
		}

	case "r":
		err := moveRook(false, fromX, fromY, toX, toY, &playerGameState, board)

		if err != nil {
			return err
		}

	case "k":

		err := moveking(false, fromX, fromY, toX, toY, &playerGameState, board)

		if err != nil {
			return err
		}

	case "n":
		err := moveKnight(false, fromX, fromY, toX, toY, &playerGameState, board)

		if err != nil {
			return err
		}

	case "P":

		err := movePawn(true, fromX, fromY, toX, toY, &playerGameState, &opponentGameState, board)

		if err != nil {
			return err
		}

	case "Q":
		err := moveQueen(true, fromX, fromY, toX, toY, &playerGameState, board)

		if err != nil {
			return err
		}

	case "B":
		err := moveBishop(true, fromX, fromY, toX, toY, &playerGameState, board)

		if err != nil {
			return err
		}
	case "R":
		err := moveRook(true, fromX, fromY, toX, toY, &playerGameState, board)

		if err != nil {
			return err
		}

	case "K":
		err := moveking(true, fromX, fromY, toX, toY, &playerGameState, board)

		if err != nil {
			return err
		}

	case "N":
		err := moveKnight(true, fromX, fromY, toX, toY, &playerGameState, board)

		if err != nil {
			return err
		}

	default:
		return errors.New("Invalid piece")
	}

	newBoardNotation, err := GetFenNotation(*board)
	if err != nil {
		return err
	}

	game.Board = *newBoardNotation
	if game.PlayerTurn == 1 {
		game.PlayerTurn = 2
	} else {
		game.PlayerTurn = 1
	}

	if err := db.DB.Save(&game).Error; err != nil {
		return err
	}

	if err := db.DB.Save(&playerGameState).Error; err != nil {
		return err
	}

	message.Board = *newBoardNotation
	message.Turn = game.PlayerTurn

	return nil
}

func getIntersection(arr1, arr2 [][]int) [][]int {
	seen := make(map[string]bool)
	var intersection [][]int

	toKey := func(slice []int) string {
		return fmt.Sprintf("%v", slice)
	}

	for _, slice := range arr1 {
		seen[toKey(slice)] = true
	}

	for _, slice := range arr2 {
		if seen[toKey(slice)] {
			intersection = append(intersection, slice)
		}
	}

	return intersection
}

func isValidMove(isWhite bool, posX int, posY int, validMoves [][]int, board *[8][8]string) bool {
	checkMoves := getCheckMoves(isWhite, board)

	if len(checkMoves) > 0 {
		validMoves = getIntersection(validMoves, checkMoves)
	}

	found := false

	for _, move := range validMoves {

		if move[0] == posY && move[1] == posX {
			found = true
			break
		}
	}

	return found
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

func IndexToFenNotation(row, col int) (string, error) {
	if int(math.Max(float64(row), float64(col))) > 7 {
		return "", fmt.Errorf("invalid fen coordinates")
	}

	ch := rune(col + 97)

	return fmt.Sprintf("%c%d", ch, 8-row), nil
}

func getKnightValidMoves(isWhite bool, xPos int, yPos int, board *[8][8]string) [][]int {
	var validMoves = make([][]int, 0)

	moves := [8][2]int{
		{2, 1},
		{2, -1},
		{-2, 1},
		{-2, -1},
		{1, 2},
		{1, -2},
		{-1, 2},
		{-1, -2},
	}

	for _, v := range moves {
		deltaX := xPos + v[0]
		if deltaX > 7 || deltaX < 0 {
			continue
		}

		deltaY := yPos + v[1]
		if deltaY > 7 || deltaY < 0 {
			continue
		}

		targetSquare := board[deltaY][deltaX]

		if targetSquare == " " {
			validMoves = append(validMoves, []int{deltaY, deltaX})
		} else if isWhite != (strings.ToUpper(targetSquare) == targetSquare) {
			validMoves = append(validMoves, []int{deltaY, deltaX})
		}
	}

	return validMoves
}

func getAntiDiagonalValidMoves(isWhite bool, xPos int, yPos int, board *[8][8]string) [][]int {

	validMoves := make([][]int, 0)

	for i := 1; i < 8; i++ {
		deltaX := xPos - i

		if deltaX < 0 {
			break
		}

		deltaY := yPos + i

		if deltaY > 7 {
			break
		}

		targetSquare := board[deltaY][deltaX]

		if targetSquare == " " {
			validMoves = append(validMoves, []int{deltaY, deltaX})
		} else {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) {
				validMoves = append(validMoves, []int{deltaY, deltaX})
			}

			break
		}

	}

	for i := 1; i < 8; i++ {
		deltaX := xPos + i

		if deltaX > 7 {
			break
		}

		deltaY := yPos - i

		if deltaY < 0 {
			break
		}

		targetSquare := board[deltaY][deltaX]

		if targetSquare == " " {
			validMoves = append(validMoves, []int{deltaY, deltaX})
		} else {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) {
				validMoves = append(validMoves, []int{deltaY, deltaX})
			}

			break
		}

	}

	return validMoves
}

func getDiagonalValidMoves(isWhite bool, xPos int, yPos int, board *[8][8]string) [][]int {

	validMoves := make([][]int, 0)

	for i := 1; i < 8; i++ {
		deltaX := xPos + i

		if deltaX > 7 {
			break
		}

		deltaY := yPos + i

		if deltaY > 7 {
			break
		}

		targetSquare := board[deltaY][deltaX]

		if targetSquare == " " {
			validMoves = append(validMoves, []int{deltaY, deltaX})
		} else {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) {
				validMoves = append(validMoves, []int{deltaY, deltaX})
			}

			break
		}

	}

	for i := 1; i < 8; i++ {
		deltaX := xPos - i

		if deltaX < 0 {
			break
		}

		deltaY := yPos - i

		if deltaY < 0 {
			break
		}

		targetSquare := board[deltaY][deltaX]

		if targetSquare == " " {
			validMoves = append(validMoves, []int{deltaY, deltaX})
		} else {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) {
				validMoves = append(validMoves, []int{deltaY, deltaX})
			}

			break
		}

	}

	return validMoves
}

func getVerticalValidMoves(isWhite bool, xPos int, yPos int, board *[8][8]string) [][]int {
	validMoves := make([][]int, 0)

	for i := 1; i < 8; i++ {
		deltaY := yPos + i

		if deltaY > 7 {
			break
		}

		targetSquare := board[deltaY][xPos]

		if targetSquare == " " {
			validMoves = append(validMoves, []int{deltaY, xPos})
		} else {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) {
				validMoves = append(validMoves, []int{deltaY, xPos})
			}

			break
		}

	}

	for i := 1; i < 8; i++ {
		deltaY := yPos - i

		if deltaY < 0 {
			break
		}

		targetSquare := board[deltaY][xPos]

		if targetSquare == " " {
			validMoves = append(validMoves, []int{deltaY, xPos})
		} else {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) {
				validMoves = append(validMoves, []int{deltaY, xPos})
			}

			break
		}

	}

	return validMoves
}

func getHorizontalValidMoves(isWhite bool, xPos int, yPos int, board *[8][8]string) [][]int {

	validMoves := make([][]int, 0)

	for i := 1; i < 8; i++ {

		deltaX := xPos + i

		if deltaX > 7 {
			break
		}

		targetSquare := board[yPos][deltaX]

		if targetSquare == " " {
			validMoves = append(validMoves, []int{yPos, deltaX})
		} else {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) {
				validMoves = append(validMoves, []int{yPos, deltaX})
			}

			break
		}

	}

	for i := 1; i < 8; i++ {

		deltaX := yPos - i

		if deltaX < 0 {
			break
		}

		targetSquare := board[yPos][deltaX]

		if targetSquare == " " {
			validMoves = append(validMoves, []int{yPos, deltaX})
		} else {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) {
				validMoves = append(validMoves, []int{yPos, deltaX})
			}

			break
		}

	}

	return validMoves
}

func getPawnValidMoves(isWhite bool, xPos int, yPos int, enpassantSquare string, board *[8][8]string) [][]int {

	validMoves := make([][]int, 0)

	if enpassantSquare != " " {

		enpassantSqr, err := getMoveNotationIndex(enpassantSquare)

		if err == nil {
			y := enpassantSqr[0]
			x := enpassantSqr[1]

			if isWhite {
				y--
			} else {
				y++
			}

			validMoves = append(validMoves, []int{y, x})
		}

	}

	if isWhite {
		deltaY := yPos - 1

		if deltaY >= 0 {
			targetSquare := board[deltaY][xPos]

			if targetSquare == " " {
				validMoves = append(validMoves, []int{deltaY, xPos})
			}

			if xPos+1 <= 7 {
				targetSquare = board[deltaY][xPos+1]

				if targetSquare != " " && isWhite != (strings.ToUpper(targetSquare) == targetSquare) {
					validMoves = append(validMoves, []int{deltaY, xPos + 1})
				}
			}

			if xPos-1 >= 0 {
				targetSquare = board[deltaY][xPos-1]

				if targetSquare != " " && isWhite != (strings.ToUpper(targetSquare) == targetSquare) {
					validMoves = append(validMoves, []int{deltaY, xPos - 1})
				}
			}
		}

		if deltaY-1 >= 0 {

			if deltaY >= 0 && board[deltaY][xPos] == " " && board[deltaY-1][xPos] == " " && yPos == 6 {
				validMoves = append(validMoves, []int{deltaY - 1, xPos})
			}
		}

	} else {
		deltaY := yPos + 1

		if deltaY <= 7 {
			targetSquare := board[deltaY][xPos]

			if targetSquare == " " {
				validMoves = append(validMoves, []int{deltaY, xPos})
			}

			if xPos+1 <= 7 {
				targetSquare = board[deltaY][xPos+1]

				if targetSquare != " " && isWhite != (strings.ToUpper(targetSquare) == targetSquare) {
					validMoves = append(validMoves, []int{deltaY, xPos + 1})
				}
			}

			if xPos-1 >= 0 {
				targetSquare = board[deltaY][xPos-1]

				if targetSquare != " " && isWhite != (strings.ToUpper(targetSquare) == targetSquare) {
					validMoves = append(validMoves, []int{deltaY, xPos - 1})
				}
			}

		}

		if deltaY+1 <= 7 {

			if board[deltaY][xPos] == " " && board[deltaY+1][xPos] == " " && yPos == 1 {
				validMoves = append(validMoves, []int{deltaY + 1, xPos})
			}
		}

	}

	return validMoves
}

func getKingValidMoves(isWhite bool, xPos int, yPos int, canLongCastle bool, canKingSideCastle bool, board *[8][8]string) [][]int {
	moves := [][2]int{
		{1, 1},
		{1, -1},
		{-1, 1},
		{-1, -1},
		{0, 1},
		{0, -1},
		{1, 0},
		{-1, 0},
	}

	canCastle := isVerticalSafe(isWhite, xPos, yPos, board)
	canCastle = canCastle && isHorizontalSafe(isWhite, xPos, yPos, board)
	canCastle = canCastle && isDiagonalSafe(isWhite, xPos, yPos, board)
	canCastle = canCastle && isAntiDiagonalSafe(isWhite, xPos, yPos, board)

	if canCastle && canKingSideCastle && xPos+1 <= 7 {

		isSafeMove := isVerticalSafe(isWhite, xPos+1, yPos, board)
		isSafeMove = isSafeMove && isHorizontalSafe(isWhite, xPos+1, yPos, board)
		isSafeMove = isSafeMove && isDiagonalSafe(isWhite, xPos+1, yPos, board)
		isSafeMove = isSafeMove && isAntiDiagonalSafe(isWhite, xPos+1, yPos, board)

		if isSafeMove {
			moves = append(moves, [2]int{0, 2})

		}

	}

	if canCastle && canLongCastle && xPos-1 >= 0 {

		isSafeMove := isVerticalSafe(isWhite, xPos-1, yPos, board)
		isSafeMove = isSafeMove && isHorizontalSafe(isWhite, xPos-1, yPos, board)
		isSafeMove = isSafeMove && isDiagonalSafe(isWhite, xPos-1, yPos, board)
		isSafeMove = isSafeMove && isAntiDiagonalSafe(isWhite, xPos-1, yPos, board)

		if isSafeMove {
			moves = append(moves, [2]int{0, -2})

		}

	}

	var validMoves = make([][]int, 0)

	for _, v := range moves {
		deltaY := yPos + v[0]
		if deltaY > 7 || deltaY < 0 {
			continue
		}

		deltaX := xPos + v[1]
		if deltaX > 7 || deltaX < 0 {
			continue
		}

		isSafeMove := isVerticalSafe(isWhite, deltaX, deltaY, board)
		isSafeMove = isSafeMove && isHorizontalSafe(isWhite, deltaX, deltaY, board)
		isSafeMove = isSafeMove && isDiagonalSafe(isWhite, deltaX, deltaY, board)
		isSafeMove = isSafeMove && isAntiDiagonalSafe(isWhite, deltaX, deltaY, board)

		if !isSafeMove {
			continue
		}

		targetSquare := board[deltaY][deltaX]

		if targetSquare == " " {
			validMoves = append(validMoves, []int{deltaY, deltaX})
		} else if isWhite != (strings.ToUpper(targetSquare) == targetSquare) {
			validMoves = append(validMoves, []int{deltaY, deltaX})
		}
	}

	return validMoves
}

func isVerticalSafe(isWhite bool, xPos int, yPos int, board *[8][8]string) bool {

	for i := 1; i < 8; i++ {
		deltaY := yPos + i

		if deltaY > 7 {
			break
		}

		targetSquare := board[deltaY][xPos]

		if targetSquare != " " {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) {

				if slices.Contains([]string{"r", "q"}, strings.ToLower(targetSquare)) {
					return false
				}

			}

			break
		}

	}

	for i := 1; i < 8; i++ {
		deltaY := yPos - i

		if deltaY < 0 {
			break
		}

		targetSquare := board[deltaY][xPos]
		if targetSquare != " " {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) {

				if slices.Contains([]string{"r", "q"}, strings.ToLower(targetSquare)) {
					return false
				}

			}

			break
		}

	}

	return true
}

func isHorizontalSafe(isWhite bool, xPos int, yPos int, board *[8][8]string) bool {

	for i := 1; i < 8; i++ {
		deltaX := xPos + i

		if deltaX > 7 {
			break
		}

		targetSquare := board[yPos][deltaX]

		if targetSquare != " " {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) {

				if slices.Contains([]string{"r", "q"}, strings.ToLower(targetSquare)) {
					return false
				}

			}

			break
		}

	}

	for i := 1; i < 8; i++ {
		deltaX := xPos - i

		if deltaX < 0 {
			break
		}

		targetSquare := board[yPos][deltaX]

		if targetSquare != " " {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) {

				if slices.Contains([]string{"r", "q"}, strings.ToLower(targetSquare)) {
					return false
				}

			}

			break
		}

	}

	return true
}

func isDiagonalSafe(isWhite bool, xPos int, yPos int, board *[8][8]string) bool {

	for i := 1; i < 8; i++ {
		deltaX := xPos + i

		if deltaX > 7 {
			break
		}

		deltaY := yPos + i

		if deltaY > 7 {
			break
		}

		targetSquare := board[deltaY][deltaX]

		if targetSquare != " " {

			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) {
				if slices.Contains([]string{"b", "q"}, strings.ToLower(targetSquare)) {
					return false
				}
			}

			break
		}

	}

	for i := 1; i < 8; i++ {
		deltaX := xPos - i

		if deltaX < 0 {
			break
		}

		deltaY := yPos - i

		if deltaY < 0 {
			break
		}

		targetSquare := board[deltaY][deltaX]

		if targetSquare != " " {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) {
				if slices.Contains([]string{"b", "q"}, strings.ToLower(targetSquare)) {
					return false
				}
			}

			break
		}
	}

	return true
}

func isAntiDiagonalSafe(isWhite bool, xPos int, yPos int, board *[8][8]string) bool {

	for i := 1; i < 8; i++ {
		deltaX := xPos + i

		if deltaX > 7 {
			break
		}

		deltaY := yPos - i

		if deltaY < 0 {
			break
		}

		targetSquare := board[deltaY][deltaX]

		if targetSquare != " " {

			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) {
				if slices.Contains([]string{"b", "q"}, strings.ToLower(targetSquare)) {
					return false
				}
			}

			break
		}

	}

	for i := 1; i < 8; i++ {
		deltaX := xPos - i

		if deltaX < 0 {
			break
		}

		deltaY := yPos + i

		if deltaY > 7 {
			break
		}

		targetSquare := board[deltaY][deltaX]

		if targetSquare != " " {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) {
				if slices.Contains([]string{"b", "q"}, strings.ToLower(targetSquare)) {
					return false
				}
			}

			break
		}
	}

	return true
}

func isKnightSafe(isWhite bool, xPos int, yPos int, board *[8][8]string) bool {

	moves := [8][2]int{
		{2, 1},
		{2, -1},
		{-2, 1},
		{-2, -1},
		{1, 2},
		{1, -2},
		{-1, 2},
		{-1, -2},
	}

	for _, v := range moves {
		deltaX := xPos + v[0]
		if deltaX > 7 || deltaX < 0 {
			continue
		}

		deltaY := yPos + v[1]
		if deltaY > 7 || deltaY < 0 {
			continue
		}

		targetSquare := board[deltaY][deltaX]

		if targetSquare != " " && isWhite != (strings.ToUpper(targetSquare) == targetSquare) {
			if slices.Contains([]string{"n"}, strings.ToLower(targetSquare)) {
				return false
			}
		}

	}

	return true
}

func isKingSafe(isWhite bool, xPos int, yPos int, board *[8][8]string) bool {

	moves := [8][2]int{
		{1, 1},
		{1, -1},
		{-1, 1},
		{-1, -1},
		{0, 1},
		{0, -1},
		{1, 0},
		{-1, 0},
	}

	for _, v := range moves {
		deltaX := xPos + v[0]
		if deltaX > 7 || deltaX < 0 {
			continue
		}

		deltaY := yPos + v[1]
		if deltaY > 7 || deltaY < 0 {
			continue
		}

		targetSquare := board[deltaY][deltaX]

		if targetSquare != " " && isWhite != (strings.ToUpper(targetSquare) == targetSquare) {
			if slices.Contains([]string{"k"}, strings.ToLower(targetSquare)) {
				return false
			}
		}

	}

	return true
}

func isPawnSafe(isWhite bool, xPos int, yPos int, board *[8][8]string) bool {

	var moves [2][2]int

	if isWhite {
		moves = [2][2]int{
			{-1, 1},
			{-1, -1},
		}
	} else {
		moves = [2][2]int{
			{1, 1},
			{1, -1},
		}
	}

	for _, v := range moves {
		deltaX := xPos + v[0]
		if deltaX > 7 || deltaX < 0 {
			continue
		}

		deltaY := yPos + v[1]
		if deltaY > 7 || deltaY < 0 {
			continue
		}

		targetSquare := board[deltaY][deltaX]

		if targetSquare != " " && isWhite != (strings.ToUpper(targetSquare) == targetSquare) {
			if slices.Contains([]string{"p"}, strings.ToLower(targetSquare)) {
				return false
			}
		}

	}

	return true
}

func validateMove(isWhite bool, board *[8][8]string) error {
	checkMoves := getCheckMoves(isWhite, board)

	if len(checkMoves) > 0 {
		return errors.New("Invalid Move")
	}

	return nil

}

func movePawn(isWhite bool, fromX int, fromY int, toX int, toY int, gameState *models.GameState, opponentGameState *models.GameState, board *[8][8]string) error {

	if !isValidMove(isWhite, toX, toY, getPawnValidMoves(isWhite, fromX, fromY, opponentGameState.Enpassant, board), board) {
		return errors.New("Invalid move")
	}

	diff := math.Abs(float64(toY) - float64(fromY))
	gameState.Enpassant = " "

	piece := board[fromY][fromX]

	if diff == 2 {
		// generated en passent move

		sqr, err := IndexToFenNotation(toY, toX)

		if err == nil {
			gameState.Enpassant = sqr
		}

	} else if fromX != fromY {
		sqr := board[toY][toX]

		if sqr == " " {
			// en passent capture

			board[fromY][toX] = " "

		}
	}

	// pawn pass and capture moves
	board[fromY][fromX] = " "
	board[toY][toX] = piece

	return validateMove(isWhite, board)
}

func moveking(isWhite bool, fromX int, fromY int, toX int, toY int, gameState *models.GameState, board *[8][8]string) error {

	found := false

	validMoves := getKingValidMoves(isWhite, fromX, fromY, gameState.CanLongCastle, gameState.CanKingSideCastle, board)

	for _, move := range validMoves {

		if move[0] == toY && move[1] == toX {
			found = true
			break
		}
	}

	if !found {

		return errors.New("Invalid move")
	}

	gameState.CanLongCastle = false
	gameState.CanKingSideCastle = false

	gameState.Enpassant = " "

	diff := math.Abs(float64(toX) - float64(fromX))
	piece := board[fromY][fromX]

	if diff == 2 {
		// castle move

		dir := -1
		rookXPos := 0

		if toX > fromX {
			dir = 1
			rookXPos = 7
		}

		for i := 1; i < 5; i++ {
			deltaX := fromX + i*dir

			if deltaX > 7 || deltaX < 0 {
				return errors.New("invalid castle")
			} else {
				sqr := board[fromY][deltaX]

				if sqr != " " {
					if isWhite {

						if sqr != "R" {
							return errors.New("invalid castle")

						}

					} else {

						if sqr != "r" {
							return errors.New("invalid castle")

						}

					}
					break
				}
			}
		}

		rook := board[fromY][rookXPos]

		isSafeMove := isHorizontalSafe(isWhite, fromX, fromY, board) && isHorizontalSafe(isWhite, fromX+1*dir, fromY, board) && isHorizontalSafe(isWhite, fromX+2*dir, fromY, board)
		isSafeMove = isSafeMove && isVerticalSafe(isWhite, fromX, fromY, board) && isVerticalSafe(isWhite, fromX+1*dir, fromY, board) && isVerticalSafe(isWhite, fromX+2*dir, fromY, board)
		isSafeMove = isSafeMove && isDiagonalSafe(isWhite, fromX, fromY, board) && isDiagonalSafe(isWhite, fromX+1*dir, fromY, board) && isDiagonalSafe(isWhite, fromX+2*dir, fromY, board)
		isSafeMove = isSafeMove && isAntiDiagonalSafe(isWhite, fromX, fromY, board) && isAntiDiagonalSafe(isWhite, fromX+1*dir, fromY, board) && isAntiDiagonalSafe(isWhite, fromX+2*dir, fromY, board)
		isSafeMove = isSafeMove && isKnightSafe(isWhite, fromX, fromY, board) && isKnightSafe(isWhite, fromX+1*dir, fromY, board) && isKnightSafe(isWhite, fromX+2*dir, fromY, board)
		isSafeMove = isSafeMove && isKingSafe(isWhite, fromX, fromY, board) && isKingSafe(isWhite, fromX+1*dir, fromY, board) && isKingSafe(isWhite, fromX+2*dir, fromY, board)
		isSafeMove = isSafeMove && isPawnSafe(isWhite, fromX, fromY, board) && isPawnSafe(isWhite, fromX+1*dir, fromY, board) && isPawnSafe(isWhite, fromX+2*dir, fromY, board)

		if !isSafeMove {
			return errors.New("Invalid move")
		}

		dir *= -1

		board[fromY][fromX] = " "
		board[fromY][rookXPos] = " "
		board[toY][toX] = piece
		board[toY][toX+1*dir] = rook

	} else {
		board[fromY][fromX] = " "
		board[toY][toX] = piece

	}

	return nil
}

func moveKnight(isWhite bool, fromX int, fromY int, toX int, toY int, gameState *models.GameState, board *[8][8]string) error {

	if !isValidMove(isWhite, toX, toY, getKnightValidMoves(isWhite, fromX, fromY, board), board) {

		return errors.New("Invalid move")
	}
	gameState.Enpassant = " "

	piece := board[fromY][fromX]
	board[fromY][fromX] = " "
	board[toY][toX] = piece

	return validateMove(isWhite, board)
}

func moveQueen(isWhite bool, fromX int, fromY int, toX int, toY int, gameState *models.GameState, board *[8][8]string) error {
	validMoves := make([][]int, 0)

	validMoves = append(validMoves, getVerticalValidMoves(isWhite, fromX, fromY, board)...)
	validMoves = append(validMoves, getHorizontalValidMoves(isWhite, fromX, fromY, board)...)
	validMoves = append(validMoves, getDiagonalValidMoves(isWhite, fromX, fromY, board)...)
	validMoves = append(validMoves, getAntiDiagonalValidMoves(isWhite, fromX, fromY, board)...)

	if !isValidMove(isWhite, toX, toY, validMoves, board) {

		return errors.New("Invalid move")
	}

	gameState.Enpassant = " "

	piece := board[fromY][fromX]
	board[fromY][fromX] = " "
	board[toY][toX] = piece

	return validateMove(isWhite, board)
}

func moveBishop(isWhite bool, fromX int, fromY int, toX int, toY int, gameState *models.GameState, board *[8][8]string) error {
	validMoves := make([][]int, 0)

	validMoves = append(validMoves, getDiagonalValidMoves(isWhite, fromX, fromY, board)...)
	validMoves = append(validMoves, getAntiDiagonalValidMoves(isWhite, fromX, fromY, board)...)

	if !isValidMove(isWhite, toX, toY, validMoves, board) {

		return errors.New("Invalid move")
	}

	gameState.Enpassant = " "

	piece := board[fromY][fromX]
	board[fromY][fromX] = " "
	board[toY][toX] = piece

	return validateMove(isWhite, board)
}

func moveRook(isWhite bool, fromX int, fromY int, toX int, toY int, gameState *models.GameState, board *[8][8]string) error {
	validMoves := make([][]int, 0)

	validMoves = append(validMoves, getVerticalValidMoves(isWhite, fromX, fromY, board)...)
	validMoves = append(validMoves, getHorizontalValidMoves(isWhite, fromX, fromY, board)...)

	if !isValidMove(isWhite, toX, toY, validMoves, board) {

		return errors.New("Invalid move")
	}

	switch fromX {
	case 7:
		gameState.CanLongCastle = false
	case 0:
		gameState.CanKingSideCastle = false
	}

	gameState.Enpassant = " "

	piece := board[fromY][fromX]
	board[fromY][fromX] = " "
	board[toY][toX] = piece

	return validateMove(isWhite, board)
}

func getKingPosition(isWhite bool, board *[8][8]string) (int, int) {

	targetKing := "k"

	if isWhite {
		targetKing = "K"
	}

	xPos := 0
	yPos := 0

	for y := 0; y < 8; y++ {
		found := false
		for x := 0; x < 8; x++ {
			if board[y][x] == targetKing {
				xPos = x
				yPos = y
				found = true
				break
			}
		}

		if found {
			break
		}
	}

	return xPos, yPos

}

func getVerticalCheckMoves(isWhite bool, board *[8][8]string) [][]int {

	xPos, yPos := getKingPosition(isWhite, board)
	checkMoves := make([][]int, 0)

	for i := 1; i < 8; i++ {

		deltaY := yPos + i

		if deltaY > 7 {
			break
		}

		targetSquare := board[deltaY][xPos]

		if targetSquare == " " {
			checkMoves = append(checkMoves, []int{deltaY, xPos})
		} else {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) && slices.Contains([]string{"q", "r"}, strings.ToLower(targetSquare)) {
				checkMoves = append(checkMoves, []int{deltaY, xPos})

				return checkMoves

			}

			break
		}

	}

	checkMoves = make([][]int, 0)

	for i := 1; i < 8; i++ {
		deltaY := yPos - i

		if deltaY < 0 {
			break
		}

		targetSquare := board[deltaY][xPos]

		if targetSquare == " " {
			checkMoves = append(checkMoves, []int{deltaY, xPos})
		} else {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) && slices.Contains([]string{"q", "r"}, strings.ToLower(targetSquare)) {
				checkMoves = append(checkMoves, []int{deltaY, xPos})

				return checkMoves
			}

			break
		}

	}

	return [][]int{}

}

func getHorizontalCheckMoves(isWhite bool, board *[8][8]string) [][]int {

	xPos, yPos := getKingPosition(isWhite, board)
	checkMoves := make([][]int, 0)

	for i := 1; i < 8; i++ {

		deltaX := xPos + i

		if deltaX > 7 {
			break
		}

		targetSquare := board[yPos][deltaX]

		if targetSquare == " " {
			checkMoves = append(checkMoves, []int{yPos, deltaX})
		} else {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) && slices.Contains([]string{"q", "r"}, strings.ToLower(targetSquare)) {
				checkMoves = append(checkMoves, []int{yPos, deltaX})

				return checkMoves

			}

			break
		}

	}

	checkMoves = make([][]int, 0)

	for i := 1; i < 8; i++ {
		deltaX := xPos - i

		if deltaX < 0 {
			break
		}

		targetSquare := board[yPos][deltaX]

		if targetSquare == " " {
			checkMoves = append(checkMoves, []int{yPos, deltaX})
		} else {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) && slices.Contains([]string{"q", "r"}, strings.ToLower(targetSquare)) {
				checkMoves = append(checkMoves, []int{yPos, deltaX})

				return checkMoves
			}

			break
		}

	}

	return [][]int{}

}

func getDiagonalCheckMoves(isWhite bool, board *[8][8]string) [][]int {

	xPos, yPos := getKingPosition(isWhite, board)
	checkMoves := make([][]int, 0)

	for i := 1; i < 8; i++ {
		deltaX := xPos + i

		if deltaX > 7 {
			break
		}

		deltaY := yPos + i

		if deltaY > 7 {
			break
		}

		targetSquare := board[deltaY][deltaX]

		if targetSquare == " " {
			checkMoves = append(checkMoves, []int{deltaY, deltaX})
		} else {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) && slices.Contains([]string{"q", "b"}, strings.ToLower(targetSquare)) {
				checkMoves = append(checkMoves, []int{deltaY, deltaX})

				return checkMoves
			}

			break
		}

	}

	checkMoves = make([][]int, 0)

	for i := 1; i < 8; i++ {
		deltaX := xPos - i

		if deltaX < 0 {
			break
		}

		deltaY := yPos - i

		if deltaY < 0 {
			break
		}

		targetSquare := board[deltaY][deltaX]

		if targetSquare == " " {
			checkMoves = append(checkMoves, []int{deltaY, deltaX})
		} else {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) && slices.Contains([]string{"q", "b"}, strings.ToLower(targetSquare)) {
				checkMoves = append(checkMoves, []int{deltaY, deltaX})

				return checkMoves
			}

			break
		}

	}

	return [][]int{}
}

func getAntiDiagonalCheckMoves(isWhite bool, board *[8][8]string) [][]int {

	xPos, yPos := getKingPosition(isWhite, board)
	checkMoves := make([][]int, 0)

	for i := 1; i < 8; i++ {
		deltaX := xPos + i

		if deltaX > 7 {
			break
		}

		deltaY := yPos - i

		if deltaY < 0 {
			break
		}

		targetSquare := board[deltaY][deltaX]

		if targetSquare == " " {
			checkMoves = append(checkMoves, []int{deltaY, deltaX})
		} else {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) && slices.Contains([]string{"q", "b"}, strings.ToLower(targetSquare)) {
				checkMoves = append(checkMoves, []int{deltaY, deltaX})

				return checkMoves
			}

			break
		}

	}

	checkMoves = make([][]int, 0)

	for i := 1; i < 8; i++ {
		deltaX := xPos - i

		if deltaX < 0 {
			break
		}

		deltaY := yPos + i

		if deltaY > 7 {
			break
		}

		targetSquare := board[deltaY][deltaX]

		if targetSquare == " " {
			checkMoves = append(checkMoves, []int{deltaY, deltaX})
		} else {
			if isWhite != (strings.ToUpper(targetSquare) == targetSquare) && slices.Contains([]string{"q", "b"}, strings.ToLower(targetSquare)) {
				checkMoves = append(checkMoves, []int{deltaY, deltaX})

				return checkMoves
			}

			break
		}

	}

	return [][]int{}
}

func getKnightCheckMoves(isWhite bool, board *[8][8]string) [][]int {
	xPos, yPos := getKingPosition(isWhite, board)

	moves := [8][2]int{
		{2, 1},
		{2, -1},
		{-2, 1},
		{-2, -1},
		{1, 2},
		{1, -2},
		{-1, 2},
		{-1, -2},
	}

	for _, v := range moves {
		deltaX := xPos + v[0]
		if deltaX > 7 || deltaX < 0 {
			continue
		}

		deltaY := yPos + v[1]
		if deltaY > 7 || deltaY < 0 {
			continue
		}

		targetSquare := board[deltaY][deltaX]

		if targetSquare != " " && isWhite != (strings.ToUpper(targetSquare) == targetSquare) && strings.ToLower(targetSquare) == "n" {
			return [][]int{{deltaY, deltaX}}
		}
	}

	return [][]int{}
}

func getPawnCheckMoves(isWhite bool, board *[8][8]string) [][]int {
	xPos, yPos := getKingPosition(isWhite, board)

	moves := [2][2]int{
		{1, 1},
		{1, -1},
	}

	if isWhite {
		moves = [2][2]int{
			{-1, 1},
			{-1, -1},
		}
	}

	for _, v := range moves {
		deltaY := yPos + v[0]
		if deltaY > 7 || deltaY < 0 {
			continue
		}

		deltaX := xPos + v[1]
		if deltaX > 7 || deltaX < 0 {
			continue
		}

		targetSquare := board[deltaY][deltaX]

		if targetSquare != " " && isWhite != (strings.ToUpper(targetSquare) == targetSquare) && strings.ToLower(targetSquare) == "p" {
			return [][]int{{deltaY, deltaX}}
		}
	}

	return [][]int{}
}

func getCheckMoves(isWhite bool, board *[8][8]string) [][]int {

	checkMoves := make([][]int, 0)
	checkMoves = append(checkMoves, getVerticalCheckMoves(isWhite, board)...)
	checkMoves = append(checkMoves, getHorizontalCheckMoves(isWhite, board)...)
	checkMoves = append(checkMoves, getDiagonalCheckMoves(isWhite, board)...)
	checkMoves = append(checkMoves, getAntiDiagonalCheckMoves(isWhite, board)...)
	checkMoves = append(checkMoves, getKnightCheckMoves(isWhite, board)...)
	checkMoves = append(checkMoves, getPawnCheckMoves(isWhite, board)...)

	return checkMoves
}

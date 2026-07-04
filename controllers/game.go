package controllers

import (
	"chess_server/database"
	"chess_server/models"
	"fmt"
	"log"
	"math"
	"net/http"
	"time"

	"chess_server/utils"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/notnil/chess"
	"github.com/notnil/chess/uci"
)

type TestWSRequest struct {
	Message string `json:"message" binding:"required,omitempty"`
}

func GetGameTypes(c *gin.Context) {
	var gameTypes []models.GameType
	result := db.DB.Find(&gameTypes)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Error retrieving game types"})
		return
	}

	type GameTypeResponse struct {
		ID       uint   `json:"id"`
		Name     string `json:"name"`
		Duration uint   `json:"duration"`
	}

	responses := make([]GameTypeResponse, 0, len(gameTypes))
	for _, gt := range gameTypes {
		responses = append(responses, GameTypeResponse{
			ID:       gt.ID,
			Name:     gt.Name,
			Duration: gt.Duration,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "game types",
		"data":    responses,
	})
}

func PlayGame(c *gin.Context) {
	token := c.Query("token")
	claims, err := utils.ValidateToken(token)

	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token", "details": err.Error()})
		c.Abort()
		return
	}
	var user models.User
	result := db.DB.First(&user, claims["id"])
	if result.Error != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
		c.Abort()
		return
	}

	gameTypeId, _ := strconv.Atoi(c.Param("id"))
	utils.EnqueuePlayer(user.ID, gameTypeId)

	utils.HandleConnection(user.ID, c.Writer, c.Request)
}

func ReConnect(c *gin.Context) {
	token := c.Query("token")
	claims, err := utils.ValidateToken(token)

	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token", "details": err.Error()})
		c.Abort()
		return
	}
	var user models.User
	result := db.DB.First(&user, claims["id"])
	if result.Error != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
		c.Abort()
		return
	}
	gameId, _ := strconv.Atoi(c.Param("id"))

	utils.HandleReConnection(user.ID, gameId, c.Writer, c.Request)
}

type GameAnalysis struct {
	Move            string  `json:"move"`
	Board           string  `json:"board"`
	Score           float64 `json:"score"`
	From            string  `json:"from"`
	To              string  `json:"to"`
	CastlingStatus  string  `json:"castling_status"`
	EnpassantSquare string  `json:"enpassant_square"`
	MoveClass       string  `json:"move_class"`
}

type GameAnalysisResponse struct {
	GameStatus          string                    `json:"game_status"`
	Username            string                    `json:"username"`
	OpponentUserName    string                    `json:"opponent_username"`
	PointsDelta         int                       `json:"points_delta"`
	OpponentPointsDelta int                       `json:"opponent_points_delta"`
	GameAnalysis        []GameAnalysis            `json:"game_analysis"`
	PlayerSummary       MoveClassificationSummary `json:"player_summary"`
	OpponentSummary     MoveClassificationSummary `json:"opponent_summary"`
	Rating              int                       `json:"rating"`
	OpponentRating      int                       `json:"opponent_rating"`
	Color               string                    `json:"color"`
}

type MoveClassificationSummary struct {
	Best       int `json:"best"`
	Excellent  int `json:"excellent"`
	Good       int `json:"good"`
	Inaccuracy int `json:"inaccuracy"`
	Mistake    int `json:"mistake"`
	Blunder    int `json:"blunder"`
	Total      int `json:"total"`
}

const startingFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"

func ClassifyMove(loss float64) string {
	switch {
	case loss <= 0.05:
		return "best"
	case loss <= 0.15:
		return "excellent"
	case loss <= 0.50:
		return "good"
	case loss <= 1.00:
		return "inaccuracy"
	case loss <= 3.00:
		return "mistake"
	default:
		return "blunder"
	}
}

func IncrementSummary(summary *MoveClassificationSummary, moveClass string) {
	summary.Total++

	switch moveClass {
	case "best":
		summary.Best++
	case "excellent":
		summary.Excellent++
	case "good":
		summary.Good++
	case "inaccuracy":
		summary.Inaccuracy++
	case "mistake":
		summary.Mistake++
	case "blunder":
		summary.Blunder++
	}
}

func EvaluationLoss(best, played float64, whiteToMove bool) float64 {
	if whiteToMove {
		return math.Max(0, best-played)
	}

	return math.Max(0, played-best)
}

func buildFEN(board, castling, enpassant, side string, fullmove int) string {
	if castling == "" {
		castling = "-"
	}

	if enpassant == "" || enpassant == " " {
		enpassant = "-"
	}

	return fmt.Sprintf("%s %s %s %s %d %d", board, side, castling, enpassant, 0, fullmove)
}

func evalPosition(eng *uci.Engine, fenString string) (float64, *chess.Move, error) {
	fen, err := chess.FEN(fenString)
	if err != nil {
		return 0, nil, fmt.Errorf("invalid FEN %q: %w", fenString, err)
	}

	g := chess.NewGame(fen)

	cmdPos := uci.CmdPosition{Position: g.Position()}
	cmdGo := uci.CmdGo{Depth: 15, MoveTime: time.Millisecond * 500}

	if err := eng.Run(cmdPos, cmdGo); err != nil {
		return 0, nil, fmt.Errorf("engine run failed: %w", err)
	}

	results := eng.SearchResults()

	var score float64
	if results.Info.Score.Mate == 0 {
		cp := float64(results.Info.Score.CP)
		score = (20.0 / (1.0 + math.Exp(-0.0036247*cp))) - 10.0
		score = math.Round(score*100) / 100
	} else if results.Info.Score.Mate > 0 {
		score = 10.0
	} else {
		score = -10.0
	}

	return score, results.BestMove, nil
}

func AnalyzeGame(c *gin.Context) {

	userObj, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		c.Abort()
		return
	}

	user, ok := userObj.(models.User)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid user context"})
		c.Abort()
		return
	}

	var opponent models.User

	username := user.UserName
	gameId, err := strconv.Atoi(c.Param("id"))

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid game ID format"})
		return
	}

	var game models.Game

	if err := db.DB.Where("id = ?", gameId).First(&game).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not find the game"})
		return
	}

	gameStatus := "draw"

	if game.Status != "draw" {
		if game.WinnerID != nil && *game.WinnerID == user.ID {
			gameStatus = "win"
		} else {
			gameStatus = "defeat"
		}
	}

	color := "black"

	opponentID := game.Player1ID
	pointsDelta := game.Player2PointsDelta
	opponentPointsDelta := game.Player1PointsDelta

	rating := game.Player2Rating
	opponentRating := game.Player1Rating

	if user.ID == opponentID {
		color = "white"
		opponentID = game.Player2ID
		pointsDelta = game.Player1PointsDelta
		opponentPointsDelta = game.Player2PointsDelta

		rating = game.Player1Rating
		opponentRating = game.Player2Rating
	}

	result := db.DB.First(&opponent, opponentID)
	if result.Error != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid opponent id"})
		c.Abort()
		return
	}

	opponentUserName := opponent.UserName

	var gameMoves []models.GameMove
	if err := db.DB.Where("game_id = ?", gameId).Order("id ASC").Find(&gameMoves).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not find the game moves"})
		return
	}

	eng, err := uci.New("stockfish")
	if err != nil {
		log.Printf("Failed to initialize Stockfish binary: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Engine initialization failed"})
		return
	}
	defer eng.Close()

	if err := eng.Run(uci.CmdUCI, uci.CmdIsReady, uci.CmdUCINewGame); err != nil {
		log.Printf("UCI setup error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Engine setup failed"})
		return
	}

	resp := make([]GameAnalysis, 0, len(gameMoves))

	var playerSummary MoveClassificationSummary
	var opponentSummary MoveClassificationSummary

	for i := range gameMoves {
		t := "b"
		if i%2 != 0 {
			t = "w"
		}

		postFullmove := i/2 + 1
		if i%2 != 0 {
			postFullmove = i/2 + 2
		}

		castlingStatus := "-"
		if gameMoves[i].CastlingStatus != nil {
			castlingStatus = *gameMoves[i].CastlingStatus
		}

		enPassSqr := "-"
		if gameMoves[i].EnpassantSquare != nil && *gameMoves[i].EnpassantSquare != " " {
			enPassSqr = *gameMoves[i].EnpassantSquare
		}

		postFEN := buildFEN(gameMoves[i].Board, castlingStatus, enPassSqr, t, postFullmove)

		sideBeforeMove := "w"
		if i%2 != 0 {
			sideBeforeMove = "b"
		}
		fullmoveBeforeMove := i/2 + 1

		var prevFEN string
		if i == 0 {
			prevFEN = startingFEN
		} else {
			prevCastling := "-"
			if gameMoves[i-1].CastlingStatus != nil {
				prevCastling = *gameMoves[i-1].CastlingStatus
			}
			prevEnpassant := "-"
			if gameMoves[i-1].EnpassantSquare != nil && *gameMoves[i-1].EnpassantSquare != " " {
				prevEnpassant = *gameMoves[i-1].EnpassantSquare
			}
			prevFEN = buildFEN(gameMoves[i-1].Board, prevCastling, prevEnpassant, sideBeforeMove, fullmoveBeforeMove)
		}

		var finalScore float64
		var moveClass string
		alreadyAnalyzed := gameMoves[i].CentiPawn != nil && gameMoves[i].MoveClass != nil

		if alreadyAnalyzed {
			// Nothing changed here: fully cached result, skip the engine.
			finalScore = *gameMoves[i].CentiPawn
			moveClass = *gameMoves[i].MoveClass
		} else {
			fen, err := chess.FEN(postFEN)
			if err != nil {
				log.Printf("Invalid FEN syntax generated on move %d: %v", i, err)
				continue
			}

			g := chess.NewGame(fen)

			if g.Outcome() != chess.NoOutcome {
				var terminalScore float64
				switch g.Outcome() {
				case chess.WhiteWon:
					terminalScore = 10.0
				case chess.BlackWon:
					terminalScore = -10.0
				case chess.Draw:
					terminalScore = 0.0
				}

				finalScore = terminalScore
				moveClass = "best"

				gameMoves[i].CentiPawn = &terminalScore
				mc := moveClass
				gameMoves[i].MoveClass = &mc

			} else {
				playedEval, _, err := evalPosition(eng, postFEN)
				if err != nil {
					log.Printf("Engine calculation failed on move %d: %v", i, err)
					continue
				}
				finalScore = playedEval
				gameMoves[i].CentiPawn = &playedEval

				bestEval, _, err := evalPosition(eng, prevFEN)
				if err != nil {
					log.Printf("Engine calculation failed evaluating pre-move position for move %d: %v", i, err)
					continue
				}

				whiteJustMoved := i%2 == 0

				loss := EvaluationLoss(bestEval, playedEval, whiteJustMoved)
				moveClass = ClassifyMove(loss)
				mc := moveClass
				gameMoves[i].MoveClass = &mc
			}
		}

		isPlayersMove := gameMoves[i].PlayerID == user.ID

		if isPlayersMove {
			IncrementSummary(&playerSummary, moveClass)
		} else {
			IncrementSummary(&opponentSummary, moveClass)
		}

		resp = append(resp, GameAnalysis{
			Move:            gameMoves[i].Notation,
			Score:           finalScore,
			Board:           postFEN,
			From:            gameMoves[i].From,
			To:              gameMoves[i].To,
			CastlingStatus:  castlingStatus,
			EnpassantSquare: enPassSqr,
			MoveClass:       moveClass,
		})
	}

	if err := db.DB.Save(&gameMoves).Error; err != nil {
		log.Printf("Failed to batch update evaluation scores in DB: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save analysis results"})
		return
	}

	c.JSON(http.StatusOK, GameAnalysisResponse{
		Username:            username,
		OpponentUserName:    opponentUserName,
		GameStatus:          gameStatus,
		PointsDelta:         int(pointsDelta),
		OpponentPointsDelta: int(opponentPointsDelta),
		GameAnalysis:        resp,
		PlayerSummary:       playerSummary,
		OpponentSummary:     opponentSummary,
		Rating:              int(rating),
		OpponentRating:      int(opponentRating),
		Color:               color,
	})
}

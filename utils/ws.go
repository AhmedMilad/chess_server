package utils

import (
	db "chess_server/database"
	"chess_server/models"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

type Session struct {
	Conn           *websocket.Conn
	LastSeen       time.Time
	IsDisconnected bool
}

var Sessions = make(map[uint]Session)
var Upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // Allow all connections
}

type Message struct {
	GameID              int             `json:"game_id"`
	Type                string          `json:"type"`
	Status              string          `json:"status"`
	Color               string          `json:"color"`
	Data                json.RawMessage `json:"data"`
	Board               string          `json:"board"`
	Turn                int             `json:"turn"`
	CangLongCastle      bool            `json:"can_long_castle"`
	CanKingSideCastle   bool            `json:"can_king_side_castle"`
	EnpassantSquare     string          `json:"enpassant_square"`
	OpponentInfo        PlayerInfo      `json:"opponent_info"`
	MyInfo              PlayerInfo      `json:"my_info"`
	MyTime              uint64          `json:"my_time"`
	OpponentTime        uint64          `json:"opponent_time"`
	MoveNotation        string          `json:"move_notation"`
	Moves               []MovesHistory  `json:"movesHistory"`
	PromoteTo           string          `json:"promote_to"`
	MyPointsDelta       float64         `json:"my_points_delta"`
	OpponentPointsDelta float64         `json:"opponent_points_delta"`
	IsDrawAvailable     bool            `json:"is_draw_available"`
	IsDrawOffered       bool            `json:"is_draw_offered"`
	IsRematchOffered    bool            `json:"is_rematch_offered"`
	IsRematchAvailable  bool            `json:"is_rematch_available"`
	IsNewGamePending    bool            `json:"is_new_game_pending"`
}

type MovesHistory struct {
	Board        string `json:"board"`
	From         string `json:"from"`
	To           string `json:"to"`
	MoveNotation string `json:"move_notation"`
}

func HandleConnection(playerID uint, w http.ResponseWriter, r *http.Request) {
	ws, err := Upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade failed for player %d: %v", playerID, err)
		return
	}
	defer func() {
		removeSessionIfCurrent(playerID, ws)
		ws.Close()
	}()

	RegisterConnection(playerID, ws)
	HandleSocketMessages(playerID, ws)
}

func HandleReConnection(playerID uint, gameId int, w http.ResponseWriter, r *http.Request) {
	ws, err := Upgrader.Upgrade(w, r, nil)

	if err != nil {
		log.Printf("upgrade failed for player %d: %v", playerID, err)
		return
	}

	defer func() {
		removeSessionIfCurrent(playerID, ws)
		ws.Close()
	}()

	removeSessionIfCurrent(playerID, ws)
	RegisterConnection(playerID, ws)

	var game models.Game
	if err := db.DB.Preload("Player1").Preload("Player2").First(&game, gameId).Error; err != nil {
		log.Println("Game not found:", err)
		return
	}

	color := "white"

	if game.Player1ID != playerID {
		color = "black"
	}

	myTime := game.Player1RemainingTime
	opponentTime := game.Player2RemainingTime
	opponentID := game.Player2ID
	myPointsDelta := game.Player1PointsDelta
	opponentPointsDelta := game.Player2PointsDelta

	if playerID == game.Player2ID {

		myTime = game.Player2RemainingTime
		opponentTime = game.Player1RemainingTime
		opponentID = game.Player1ID

		myPointsDelta = game.Player2PointsDelta
		opponentPointsDelta = game.Player1PointsDelta

	}

	curTimeStamp := time.Now().UnixMilli()

	if game.PlayerTurn == 1 {

		calculatedTime := game.Player1RemainingTime

		if game.Status == "ongoing" {
			calculatedTime = game.Player1RemainingTime - curTimeStamp + game.Player1LastMoveAt

		}

		if playerID == game.Player1ID {
			myTime = calculatedTime
		} else {
			opponentTime = calculatedTime

		}

	} else {
		calculatedTime := game.Player2RemainingTime
		if game.Status == "ongoing" {

			calculatedTime = game.Player2RemainingTime - curTimeStamp + game.Player2LastMoveAt
		}

		if playerID == game.Player1ID {
			opponentTime = calculatedTime
		} else {
			myTime = calculatedTime

		}
	}

	if myTime < 0 {
		myTime = 0
	}
	if opponentTime < 0 {
		opponentTime = 0
	}

	status := "ok"

	if game.Status == "draw" {
		status = "draw"
	}

	if game.Status == "finished" {
		if *game.WinnerID == playerID {
			status = "win"
		} else {
			status = "defeat"
		}
	}

	var gameMove models.GameMove

	db.DB.Where("game_id = ?", game.ID).Last(&gameMove)

	type moveData struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	var data *moveData

	if gameMove.From != "" && gameMove.To != "" {
		data = &moveData{
			From: gameMove.From,
			To:   gameMove.To,
		}
	}

	dataMsg, dataErr := json.Marshal(data)

	if dataErr != nil {
		log.Printf("could not marshal the data info and got the error: %s", dataErr.Error())
		return

	}

	var moves []MovesHistory
	var playerRating models.UserGameRating
	var opponentRating models.UserGameRating
	var gameMoves []models.GameMove

	db.DB.Model(&models.GameMove{}).Where("game_id = ?", game.ID).Order("id asc").Find(&gameMoves)
	db.DB.Preload("User").Where("user_id = ? AND game_type_id = ?", playerID, game.GameTypeID).First(&playerRating)
	db.DB.Preload("User").Where("user_id = ? AND game_type_id = ?", opponentID, game.GameTypeID).First(&opponentRating)

	for _, gm := range gameMoves {
		moves = append(moves, MovesHistory{
			Board:        gm.Board,
			From:         gm.From,
			To:           gm.To,
			MoveNotation: gm.Notation,
		})
	}

	isDrawAvailable := false
	isDrawOffered := false

	if game.DrawOfferedByID != nil {
		if *game.DrawOfferedByID == playerID {
			isDrawOffered = true
		} else {
			isDrawAvailable = true
		}
	}

	isRematchAvailable := false
	isRematchOffered := false

	if game.RematchOfferedByID != nil {
		if *game.RematchOfferedByID == playerID {
			isRematchOffered = true
		} else {
			isRematchAvailable = true
		}
	}

	isNewGamePending, err := IsPlayerWaiting(playerID, int(game.GameTypeID))

	if err != nil {
		log.Println(err)
		return
	}

	message := Message{
		Type:         "reconnect_game",
		GameID:       gameId,
		Board:        game.Board,
		Turn:         game.PlayerTurn,
		Color:        color,
		MyTime:       uint64(myTime),
		OpponentTime: uint64(opponentTime),
		Status:       status,
		MoveNotation: gameMove.Notation,
		Moves:        moves,
		Data:         dataMsg,
		MyInfo: PlayerInfo{
			UserName: playerRating.User.UserName,
			Rating:   strconv.FormatInt(int64(playerRating.Rating), 10),
		},
		OpponentInfo: PlayerInfo{
			UserName: opponentRating.User.UserName,
			Rating:   strconv.FormatInt(int64(opponentRating.Rating), 10),
		},
		MyPointsDelta:       myPointsDelta,
		OpponentPointsDelta: opponentPointsDelta,
		IsDrawAvailable:     isDrawAvailable,
		IsDrawOffered:       isDrawOffered,
		IsRematchOffered:    isRematchOffered,
		IsRematchAvailable:  isRematchAvailable,
		IsNewGamePending:    isNewGamePending,
	}

	oppoonetPlayerID := game.Player2ID

	if playerID == game.Player2ID {
		oppoonetPlayerID = game.Player1ID

	}

	playerGameState := models.GameState{}
	opponentGameState := models.GameState{}
	fetchErr := db.DB.Where("game_id = ? AND user_id = ?", game.ID, oppoonetPlayerID).First(&opponentGameState).Error

	if fetchErr == nil {
		message.EnpassantSquare = opponentGameState.Enpassant
	}

	fetchErr = db.DB.Where("game_id = ? AND user_id = ?", game.ID, playerID).First(&playerGameState).Error
	if fetchErr == nil {
		message.CangLongCastle = playerGameState.CanLongCastle
		message.CanKingSideCastle = playerGameState.CanKingSideCastle
	}

	msg, err := json.Marshal(message)
	if err != nil {
		log.Println("Invalid message")
	}

	PlayerMutex.Lock()
	opponentWS, ok := Sessions[playerID]
	PlayerMutex.Unlock()

	if ok {
		if err := opponentWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			opponentWS.Conn.Close()
				removeSessionIfCurrent(playerID, ws)
		}
	}

	HandleSocketMessages(playerID, ws)

}

func HandleSocketMessages(playerID uint, ws *websocket.Conn) {
	for {
		_, msg, err := ws.ReadMessage()

		if err != nil {
			log.Printf("player %d read error: %v", playerID, err)
			removeSessionIfCurrent(playerID, ws)
			break
		}

		var message Message
		var game models.Game

		if err := json.Unmarshal(msg, &message); err != nil {
			log.Println("Invalid message:", err)
			continue
		}

		if err := db.DB.First(&game, message.GameID).Error; err != nil {
			log.Println("Game not found:", err)
			continue
		}

		var opponentID, currentPlayerID uint

		if game.Player1ID == playerID {

			currentPlayerID = game.Player1ID
			opponentID = game.Player2ID
		} else if game.Player2ID == playerID {

			opponentID = game.Player1ID
			currentPlayerID = game.Player2ID
		} else {

			log.Println("Player not part of this game")
			continue
		}

		PlayerMutex.Lock()

		opponentWS, opponentOk := Sessions[opponentID]
		playerWS, playerOk := Sessions[currentPlayerID]

		PlayerMutex.Unlock()

		if message.Type == "draw" {

			if game.DrawAcceptedByID != nil && game.DrawOfferedByID != nil {
				continue
			}

			if game.DrawOfferedByID == nil {

				game.DrawOfferedByID = &playerID

				if err := db.DB.Save(&game).Error; err != nil {
					log.Println(err.Error())

					continue
				}

			} else {
				if *game.DrawOfferedByID != playerID {

					// both players agreed to draw
					game.DrawAcceptedByID = &playerID

					if err := db.DB.Save(&game).Error; err != nil {
						log.Println(err.Error())

						continue
					}

				}

			}

			opponentID := game.Player1ID

			if game.Player1ID == playerID {

				opponentID = game.Player2ID
			}

			if playerOk && opponentOk {
				if err := db.DB.Save(&game).Error; err != nil {
					log.Println(err.Error())

					continue
				}

				offerDrawMessage := Message{
					GameID: int(game.ID),
					Type:   "draw_available",
				}

				msg, err = json.Marshal(offerDrawMessage)

				if err != nil {

					log.Println("Invalid message")
					continue
				}

				if err := opponentWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					opponentWS.Conn.Close()
					removeSessionIfCurrent(opponentID, ws)
				}

				offerDrawMessage.Type = "draw_offered"

				msg, err = json.Marshal(offerDrawMessage)

				if err != nil {

					log.Println("Invalid message")
					continue
				}

				if err := playerWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					playerWS.Conn.Close()
					removeSessionIfCurrent(playerID, ws)
				}

			}

			continue
		}

		if message.Type == "cancel_draw" {

			if *game.DrawOfferedByID != playerID {

				continue
			}

			game.DrawOfferedByID = nil

			if err := db.DB.Save(&game).Error; err != nil {
				log.Println(err.Error())

				continue
			}

			opponentID := game.Player1ID

			if game.Player1ID == playerID {

				opponentID = game.Player2ID
			}

			if opponentOk && playerOk {
				if err := db.DB.Save(&game).Error; err != nil {
					log.Println(err.Error())

					continue
				}

				offerDrawMessage := Message{
					GameID:          int(game.ID),
					Type:            "cancel_draw",
					IsDrawAvailable: true,
				}

				msg, err = json.Marshal(offerDrawMessage)

				if err != nil {

					log.Println("Invalid message")
					continue
				}

				if err := playerWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					playerWS.Conn.Close()
					removeSessionIfCurrent(playerID, ws)
				}

				if err := opponentWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					opponentWS.Conn.Close()
					removeSessionIfCurrent(opponentID, ws)
				}

			}

			continue
		}

		if message.Type == "decline_draw" {

			if *game.DrawOfferedByID == playerID {

				continue
			}

			game.DrawOfferedByID = nil

			if err := db.DB.Save(&game).Error; err != nil {
				log.Println(err.Error())

				continue
			}

			opponentID := game.Player1ID

			if game.Player1ID == playerID {

				opponentID = game.Player2ID
			}

			if opponentOk && playerOk {
				if err := db.DB.Save(&game).Error; err != nil {
					log.Println(err.Error())

					continue
				}

				offerDrawMessage := Message{
					GameID:          int(game.ID),
					Type:            "decline_draw",
					IsDrawAvailable: true,
				}

				msg, err = json.Marshal(offerDrawMessage)

				if err != nil {

					log.Println("Invalid message")
					continue
				}

				if err := playerWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					playerWS.Conn.Close()
					removeSessionIfCurrent(playerID, ws)
				}

				if err := opponentWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					opponentWS.Conn.Close()
					removeSessionIfCurrent(opponentID, ws)
				}

			}

			continue
		}

		if message.Type == "accept_draw" {

			if *game.DrawOfferedByID != opponentID {
				// TODO log here
				continue
			}

			myTime := game.Player1RemainingTime
			opponentTime := game.Player2RemainingTime

			if playerID == game.Player2ID {

				myTime = game.Player2RemainingTime
				opponentTime = game.Player1RemainingTime
				opponentID = game.Player1ID

			}

			curTimeStamp := time.Now().UnixMilli()
			graceLagTime := int64(300)

			if game.PlayerTurn == 1 {

				calculatedTime := game.Player1RemainingTime

				if game.Status == "ongoing" {
					calculatedTime = game.Player1RemainingTime - curTimeStamp + game.Player1LastMoveAt

				}

				if playerID == game.Player1ID {
					myTime = calculatedTime
				} else {
					opponentTime = calculatedTime

				}

				timeTaken := curTimeStamp - game.Player1LastMoveAt
				timeSpent := int64(math.Max(0, float64(timeTaken-graceLagTime)))

				game.Player1RemainingTime = int64(math.Max(0, float64(game.Player1RemainingTime-timeSpent)))

			} else {
				calculatedTime := game.Player2RemainingTime
				if game.Status == "ongoing" {

					calculatedTime = game.Player2RemainingTime - curTimeStamp + game.Player2LastMoveAt
				}

				if playerID == game.Player1ID {
					opponentTime = calculatedTime
				} else {
					myTime = calculatedTime

				}
				timeTaken := curTimeStamp - game.Player2LastMoveAt
				timeSpent := int64(math.Max(0, float64(timeTaken-graceLagTime)))

				game.Player2RemainingTime = int64(math.Max(0, float64(game.Player2RemainingTime-timeSpent)))

			}

			if myTime < 0 {
				myTime = 0
			}
			if opponentTime < 0 {
				opponentTime = 0
			}

			game.Status = "draw"

			if err := db.DB.Save(&game).Error; err != nil {
				log.Println(err.Error())

				continue
			}

			var player1Rating models.UserGameRating
			var player2Rating models.UserGameRating

			var player1 models.User
			var player2 models.User

			if err := db.DB.Where("user_id = ? AND game_type_id = ?", currentPlayerID, game.GameTypeID).First(&player1Rating).Error; err != nil {

				log.Printf("Could not find ther user game rating with the user id %d and game type id %d while creating a game", playerID, game.GameTypeID)
				return
			}

			if err := db.DB.Where("user_id = ? AND game_type_id = ?", opponentID, game.GameTypeID).First(&player2Rating).Error; err != nil {

				log.Printf("Could not find ther user game rating with the user id %d and game type id %d while creating a game", opponentID, game.GameTypeID)
				return
			}

			db.DB.Where("id = ?", currentPlayerID).First(&player1)
			db.DB.Where("id = ?", opponentID).First(&player2)

			err := db.DB.Transaction(func(tx *gorm.DB) error {
				err := updatePlayerRating(currentPlayerID, &game, &player1Rating, tx)

				if err != nil {
					log.Println(err)

					return err
				}

				err = updatePlayerRating(opponentID, &game, &player2Rating, tx)

				if err != nil {
					log.Println(err)
					return err
				}

				return nil
			})

			if err != nil {
				log.Println("Could not update players rating")
				continue
			}

			myPointsDelta := game.Player1PointsDelta
			opponentPointsDelta := game.Player2PointsDelta

			if currentPlayerID == game.Player2ID {
				myPointsDelta = game.Player2PointsDelta
				opponentPointsDelta = game.Player1PointsDelta

			}

			drawMessage := Message{
				GameID:       int(game.ID),
				Type:         "draw",
				Board:        game.Board,
				Status:       "draw",
				Data:         message.Data,
				MyTime:       uint64(myTime),
				OpponentTime: uint64(opponentTime),
				MyInfo: PlayerInfo{
					UserName: player1.UserName,
					Rating:   strconv.FormatFloat(player1Rating.Rating, 'f', 0, 64),
				},
				OpponentInfo: PlayerInfo{
					UserName: player2.UserName,
					Rating:   strconv.FormatFloat(player2Rating.Rating, 'f', 0, 64),
				},

				MyPointsDelta:       myPointsDelta,
				OpponentPointsDelta: opponentPointsDelta,
			}

			msg, err := json.Marshal(drawMessage)

			if err != nil {

				log.Println("Invalid message")
				continue
			}

			if err := playerWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				playerWS.Conn.Close()
				removeSessionIfCurrent(playerID, ws)
			}

			drawMessage.MyTime = uint64(opponentTime)
			drawMessage.OpponentTime = uint64(myTime)

			drawMessage.MyPointsDelta = opponentPointsDelta
			drawMessage.OpponentPointsDelta = myPointsDelta

			drawMessage.MyInfo = PlayerInfo{
				UserName: player2.UserName,
				Rating:   strconv.FormatFloat(player2Rating.Rating, 'f', 0, 64),
			}

			drawMessage.OpponentInfo = PlayerInfo{
				UserName: player1.UserName,
				Rating:   strconv.FormatFloat(player1Rating.Rating, 'f', 0, 64),
			}

			msg, err = json.Marshal(drawMessage)

			if err != nil {

				log.Println("Invalid message")
				continue
			}

			if err := opponentWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				opponentWS.Conn.Close()
				removeSessionIfCurrent(opponentID, ws)
			}

			continue
		}

		if message.Type == "resign" {

			myTime := game.Player1RemainingTime
			opponentTime := game.Player2RemainingTime

			if playerID == game.Player2ID {

				myTime = game.Player2RemainingTime
				opponentTime = game.Player1RemainingTime
				opponentID = game.Player1ID

			}

			curTimeStamp := time.Now().UnixMilli()
			graceLagTime := int64(300)

			if game.PlayerTurn == 1 {

				calculatedTime := game.Player1RemainingTime

				if game.Status == "ongoing" {
					calculatedTime = game.Player1RemainingTime - curTimeStamp + game.Player1LastMoveAt

				}

				if playerID == game.Player1ID {
					myTime = calculatedTime
				} else {
					opponentTime = calculatedTime

				}

				timeTaken := curTimeStamp - game.Player1LastMoveAt
				timeSpent := int64(math.Max(0, float64(timeTaken-graceLagTime)))

				game.Player1RemainingTime = int64(math.Max(0, float64(game.Player1RemainingTime-timeSpent)))

			} else {
				calculatedTime := game.Player2RemainingTime
				if game.Status == "ongoing" {

					calculatedTime = game.Player2RemainingTime - curTimeStamp + game.Player2LastMoveAt
				}

				if playerID == game.Player1ID {
					opponentTime = calculatedTime
				} else {
					myTime = calculatedTime

				}
				timeTaken := curTimeStamp - game.Player2LastMoveAt
				timeSpent := int64(math.Max(0, float64(timeTaken-graceLagTime)))

				game.Player2RemainingTime = int64(math.Max(0, float64(game.Player2RemainingTime-timeSpent)))

			}

			if myTime < 0 {
				myTime = 0
			}
			if opponentTime < 0 {
				opponentTime = 0
			}

			game.Status = "finished"
			game.WinnerID = &opponentID

			if err := db.DB.Save(&game).Error; err != nil {
				log.Println(err.Error())

				continue
			}

			var player1Rating models.UserGameRating
			var player2Rating models.UserGameRating

			var player1 models.User
			var player2 models.User

			if err := db.DB.Where("user_id = ? AND game_type_id = ?", currentPlayerID, game.GameTypeID).First(&player1Rating).Error; err != nil {

				log.Printf("Could not find ther user game rating with the user id %d and game type id %d while creating a game", playerID, game.GameTypeID)
				return
			}

			if err := db.DB.Where("user_id = ? AND game_type_id = ?", opponentID, game.GameTypeID).First(&player2Rating).Error; err != nil {

				log.Printf("Could not find ther user game rating with the user id %d and game type id %d while creating a game", opponentID, game.GameTypeID)
				return
			}

			db.DB.Where("id = ?", currentPlayerID).First(&player1)
			db.DB.Where("id = ?", opponentID).First(&player2)

			err := db.DB.Transaction(func(tx *gorm.DB) error {
				err := updatePlayerRating(currentPlayerID, &game, &player1Rating, tx)

				if err != nil {
					log.Println(err)

					return err
				}

				err = updatePlayerRating(opponentID, &game, &player2Rating, tx)

				if err != nil {
					log.Println(err)
					return err
				}

				return nil
			})

			if err != nil {
				log.Println("Could not update players rating")
				continue
			}

			myPointsDelta := game.Player1PointsDelta
			opponentPointsDelta := game.Player2PointsDelta

			if currentPlayerID == game.Player2ID {
				myPointsDelta = game.Player2PointsDelta
				opponentPointsDelta = game.Player1PointsDelta

			}

			resignMessage := Message{
				GameID:       int(game.ID),
				Type:         "resign",
				Board:        game.Board,
				Status:       "defeat",
				Data:         message.Data,
				MyTime:       uint64(myTime),
				OpponentTime: uint64(opponentTime),
				MyInfo: PlayerInfo{
					UserName: player1.UserName,
					Rating:   strconv.FormatFloat(player1Rating.Rating, 'f', 0, 64),
				},
				OpponentInfo: PlayerInfo{
					UserName: player2.UserName,
					Rating:   strconv.FormatFloat(player2Rating.Rating, 'f', 0, 64),
				},

				MyPointsDelta:       myPointsDelta,
				OpponentPointsDelta: opponentPointsDelta,
			}

			msg, err := json.Marshal(resignMessage)

			if err != nil {

				log.Println("Invalid message")
				continue
			}

			if err := playerWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				playerWS.Conn.Close()
				removeSessionIfCurrent(playerID, ws)
			}

			resignMessage.MyTime = uint64(opponentTime)
			resignMessage.OpponentTime = uint64(myTime)
			resignMessage.Status = "win"

			resignMessage.MyPointsDelta = opponentPointsDelta
			resignMessage.OpponentPointsDelta = myPointsDelta

			resignMessage.MyInfo = PlayerInfo{
				UserName: player2.UserName,
				Rating:   strconv.FormatFloat(player2Rating.Rating, 'f', 0, 64),
			}

			resignMessage.OpponentInfo = PlayerInfo{
				UserName: player1.UserName,
				Rating:   strconv.FormatFloat(player1Rating.Rating, 'f', 0, 64),
			}

			msg, err = json.Marshal(resignMessage)

			if err != nil {

				log.Println("Invalid message")
				continue
			}

			if err := opponentWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				opponentWS.Conn.Close()
				removeSessionIfCurrent(opponentID, ws)
			}

			continue
		}

		if message.Type == "offer_rematch" {

			if game.RematchOfferedByID != nil && game.DrawOfferedByID != nil {
				continue
			}

			if game.RematchOfferedByID == nil {

				game.RematchOfferedByID = &playerID

				if err := db.DB.Save(&game).Error; err != nil {
					log.Println(err.Error())

					continue
				}

			} else {
				if *game.RematchOfferedByID != playerID {

					// both players agreed to draw
					game.RematchOfferedByID = &playerID

					if err := db.DB.Save(&game).Error; err != nil {
						log.Println(err.Error())

						continue
					}

				}

			}

			opponentID := game.Player1ID

			if game.Player1ID == playerID {

				opponentID = game.Player2ID
			}

			if playerOk && opponentOk {
				if err := db.DB.Save(&game).Error; err != nil {
					log.Println(err.Error())

					continue
				}

				offerRematchMessage := Message{
					GameID:             int(game.ID),
					Type:               "rematch_available",
					IsRematchAvailable: true,
				}

				msg, err = json.Marshal(offerRematchMessage)

				if err != nil {

					log.Println("Invalid message")
					continue
				}

				if err := opponentWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					opponentWS.Conn.Close()
					removeSessionIfCurrent(opponentID, ws)
				}

				offerRematchMessage.Type = "rematch_offered"
				offerRematchMessage.IsRematchAvailable = false
				offerRematchMessage.IsRematchOffered = true

				msg, err = json.Marshal(offerRematchMessage)

				if err != nil {

					log.Println("Invalid message")
					continue
				}

				if err := playerWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					playerWS.Conn.Close()
					removeSessionIfCurrent(playerID, ws)
				}

			}

			continue
		}

		if message.Type == "cancel_rematch" {

			if *game.RematchOfferedByID != playerID {

				continue
			}

			game.RematchOfferedByID = nil

			if err := db.DB.Save(&game).Error; err != nil {
				log.Println(err.Error())

				continue
			}

			opponentID := game.Player1ID

			if game.Player1ID == playerID {

				opponentID = game.Player2ID
			}

			if opponentOk && playerOk {
				if err := db.DB.Save(&game).Error; err != nil {
					log.Println(err.Error())

					continue
				}

				offerRematchMessage := Message{
					GameID:             int(game.ID),
					Type:               "cancel_rematch",
					IsRematchOffered:   false,
					IsRematchAvailable: false,
				}

				msg, err = json.Marshal(offerRematchMessage)

				if err != nil {

					log.Println("Invalid message")
					continue
				}

				if err := playerWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					playerWS.Conn.Close()
					removeSessionIfCurrent(playerID, ws)
				}

				if err := opponentWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					opponentWS.Conn.Close()
					removeSessionIfCurrent(opponentID, ws)
				}

			}

			continue
		}

		if message.Type == "accept_rematch" {

			p1 := Player{
				UserID: game.Player1ID,
			}

			p2 := Player{
				UserID: game.Player2ID,
			}

			game.RematchAcceptedByID = &playerID

			if err := db.DB.Save(&game).Error; err != nil {
				log.Println(err.Error())

				continue
			}

			createGame(p1, p2, game.GameTypeID)
			continue
		}

		if message.Type == "new_game" {

			EnqueuePlayer(playerID, int(game.GameTypeID))

			if playerOk {
				newGameMessage := Message{
					GameID: int(game.ID),
					Type:   "pending_new_game",
				}

				msg, err = json.Marshal(newGameMessage)

				if err != nil {

					log.Println("Invalid message")
					continue
				}

				if err := playerWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					playerWS.Conn.Close()
					removeSessionIfCurrent(playerID, ws)
				}

			}

			continue
		}

		if message.Type == "cancel_new_game" {

			err := DequeuePlayer(playerID, int(game.GameTypeID))

			if err != nil {

				log.Println(err)
				continue
			}

			if playerOk {
				cancelNewGameMessage := Message{
					GameID: int(game.ID),
					Type:   "cancel_new_game",
				}

				msg, err = json.Marshal(cancelNewGameMessage)

				if err != nil {

					log.Println("Invalid message")
					continue
				}

				if err := playerWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					playerWS.Conn.Close()
					removeSessionIfCurrent(playerID, ws)
				}

			}

			continue
		}

		// handle move logic
		if game.Status != "ongoing" || game.Player1RemainingTime == 0 {

			PlayerMutex.Lock()
			playerWS, ok := Sessions[playerID]
			PlayerMutex.Unlock()

			if ok {

				status := "win"

				if game.WinnerID != &playerID {
					status = "defeat"
				}

				message.Type = "time_out"
				message.Status = status
				message.Board = game.Board

				msg, err = json.Marshal(message)

				if err != nil {

					log.Println("Invalid message")
					continue
				}

				if err := playerWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					playerWS.Conn.Close()
					removeSessionIfCurrent(playerID, ws)
				}
			}
			continue
		}

		// player turn = 1, means white to play and 2 for black

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

		//TODO A message should be sent to the client indicating that there is no game state for the current game.
		if err != nil {
			log.Println(err.Error())
			continue
		}

		err = db.DB.Where("game_id = ? AND user_id = ?", game.ID, opponentPlayerID).First(&opponentGameState).Error

		//TODO A message should be sent to the client indicating that there is no game state for the current game.
		if err != nil {
			log.Println(err.Error())
			continue
		}

		message.Board = game.Board
		message.Turn = game.PlayerTurn
		message.EnpassantSquare = opponentGameState.Enpassant
		message.Status = "failed"

		gameMove := models.GameMove{}

		moveError := GenericHandleMove(&game, &gameMove, &playerGameState, &opponentGameState, &message)

		if moveError != nil {
			log.Println("Invalid Move Error: ", moveError)

			// send only to the player who made the action

			PlayerMutex.Lock()
			playerWS, ok := Sessions[currentPlayerID]
			PlayerMutex.Unlock()

			if ok {
				message.CanKingSideCastle = playerGameState.CanKingSideCastle
				message.CangLongCastle = playerGameState.CanLongCastle
				message.Data = nil

				msg, err = json.Marshal(message)

				if err != nil {

					log.Println("Invalid message")
					continue
				}

				if err := playerWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					playerWS.Conn.Close()
					removeSessionIfCurrent(playerID, ws)
				}
			}

			continue
		}

		game.DrawOfferedByID = nil

		if err := db.DB.Save(&game).Error; err != nil {
			log.Println(err.Error())

			continue
		}

		if err := db.DB.Save(&playerGameState).Error; err != nil {
			log.Println(err.Error())

			continue
		}

		if err := db.DB.Create(&gameMove).Error; err != nil {
			log.Println(err.Error())

			continue
		}

		message.MoveNotation = gameMove.Notation

		if game.PlayerTurn == 1 {

			UpdateWatch(game.ID, time.Duration(game.Player1RemainingTime)*time.Millisecond)
		} else {

			UpdateWatch(game.ID, time.Duration(game.Player2RemainingTime)*time.Millisecond)
		}

		if playerOk && opponentOk {
			isMate, mateErr := isCheckMate(int(opponentID), playerGameState, opponentGameState, game, message.PromoteTo)

			if mateErr != nil {
				log.Printf("Error while getting the check mate status: %s", mateErr.Error())

				continue
			}

			if isMate {

				t1 := game.Player1RemainingTime
				t2 := game.Player2RemainingTime

				gameMove.Notation += "#"

				if err := db.DB.Save(&gameMove).Error; err != nil {
					log.Println(err.Error())

					continue
				}

				if playerID == game.Player2ID {
					t1 = game.Player2RemainingTime
					t2 = game.Player1RemainingTime
				}

				game.WinnerID = &playerID
				game.Status = "finished"

				if err := db.DB.Save(&game).Error; err != nil {
					log.Println(err)
					continue
				}

				var player1Rating models.UserGameRating
				var player2Rating models.UserGameRating

				var player1 models.User
				var player2 models.User

				db.DB.Where("id = ?", currentPlayerID).First(&player1)
				db.DB.Where("id = ?", opponentID).First(&player2)

				if err := db.DB.Where("user_id = ? AND game_type_id = ?", currentPlayerID, game.GameTypeID).First(&player1Rating).Error; err != nil {

					log.Printf("Could not find ther user game rating with the user id %d and game type id %d while creating a game", playerID, game.GameTypeID)
					return
				}

				if err := db.DB.Where("user_id = ? AND game_type_id = ?", opponentID, game.GameTypeID).First(&player2Rating).Error; err != nil {

					log.Printf("Could not find ther user game rating with the user id %d and game type id %d while creating a game", opponentID, game.GameTypeID)
					return
				}

				err := db.DB.Transaction(func(tx *gorm.DB) error {
					err := updatePlayerRating(currentPlayerID, &game, &player1Rating, tx)

					if err != nil {
						log.Println(err)

						return err
					}

					err = updatePlayerRating(opponentID, &game, &player2Rating, tx)

					if err != nil {
						log.Println(err)
						return err
					}

					return nil
				})

				if err != nil {
					log.Println("Could not update players rating")
					continue
				}

				myPointsDelta := game.Player1PointsDelta
				opponentPointsDelta := game.Player2PointsDelta

				if currentPlayerID == game.Player2ID {
					myPointsDelta = game.Player2PointsDelta
					opponentPointsDelta = game.Player1PointsDelta

				}

				checkMateMessage := Message{
					GameID:       int(game.ID),
					Type:         "checkmate",
					Board:        game.Board,
					Status:       "win",
					Data:         message.Data,
					MyTime:       uint64(t1),
					OpponentTime: uint64(t2),
					MoveNotation: gameMove.Notation,
					MyInfo: PlayerInfo{
						UserName: player1.UserName,
						Rating:   strconv.FormatFloat(player1Rating.Rating, 'f', 0, 64),
					},
					OpponentInfo: PlayerInfo{
						UserName: player2.UserName,
						Rating:   strconv.FormatFloat(player2Rating.Rating, 'f', 0, 64),
					},
					MyPointsDelta:       myPointsDelta,
					OpponentPointsDelta: opponentPointsDelta,
				}

				msg, err := json.Marshal(checkMateMessage)

				if err != nil {

					log.Println("Invalid message")
					continue
				}

				if err := playerWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					playerWS.Conn.Close()
					removeSessionIfCurrent(playerID, ws)
				}

				checkMateMessage.Status = "defeat"

				checkMateMessage.MyTime = uint64(t2)
				checkMateMessage.OpponentTime = uint64(t1)

				checkMateMessage.MyInfo = PlayerInfo{
					UserName: player2.UserName,
					Rating:   strconv.FormatFloat(player2Rating.Rating, 'f', 0, 64),
				}

				checkMateMessage.OpponentInfo = PlayerInfo{
					UserName: player1.UserName,
					Rating:   strconv.FormatFloat(player1Rating.Rating, 'f', 0, 64),
				}

				checkMateMessage.MyPointsDelta = opponentPointsDelta
				checkMateMessage.OpponentPointsDelta = myPointsDelta

				msg, err = json.Marshal(checkMateMessage)

				if err != nil {

					log.Println("Invalid message")
					continue
				}

				if err := opponentWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					opponentWS.Conn.Close()
					removeSessionIfCurrent(opponentID, ws)
				}

				continue
			}

			isDraw, drawErr := isDraw(opponentID, game, playerGameState, opponentGameState)

			if drawErr != nil {
				log.Printf("Error while getting the draw status: %s", drawErr.Error())

				continue
			}

			if isDraw {

				t1 := game.Player1RemainingTime
				t2 := game.Player2RemainingTime

				gameMove.Notation += " 1/2-1/2"

				if err := db.DB.Save(&gameMove).Error; err != nil {
					log.Println(err.Error())

					continue
				}

				if playerID == game.Player2ID {
					t1 = game.Player2RemainingTime
					t2 = game.Player1RemainingTime
				}

				game.Status = "draw"

				if err := db.DB.Save(&game).Error; err != nil {
					log.Println(err.Error())

					continue
				}

				var player1Rating models.UserGameRating
				var player2Rating models.UserGameRating

				var player1 models.User
				var player2 models.User

				if err := db.DB.Where("user_id = ? AND game_type_id = ?", currentPlayerID, game.GameTypeID).First(&player1Rating).Error; err != nil {

					log.Printf("Could not find ther user game rating with the user id %d and game type id %d while creating a game", playerID, game.GameTypeID)
					return
				}

				if err := db.DB.Where("user_id = ? AND game_type_id = ?", opponentID, game.GameTypeID).First(&player2Rating).Error; err != nil {

					log.Printf("Could not find ther user game rating with the user id %d and game type id %d while creating a game", opponentID, game.GameTypeID)
					return
				}

				db.DB.Where("id = ?", currentPlayerID).First(&player1)
				db.DB.Where("id = ?", opponentID).First(&player2)

				err := db.DB.Transaction(func(tx *gorm.DB) error {
					err := updatePlayerRating(currentPlayerID, &game, &player1Rating, tx)

					if err != nil {
						log.Println(err)

						return err
					}

					err = updatePlayerRating(opponentID, &game, &player2Rating, tx)

					if err != nil {
						log.Println(err)
						return err
					}

					return nil
				})

				if err != nil {
					log.Println("Could not update players rating")
					continue
				}

				myPointsDelta := game.Player1PointsDelta
				opponentPointsDelta := game.Player2PointsDelta

				if currentPlayerID == game.Player2ID {
					myPointsDelta = game.Player2PointsDelta
					opponentPointsDelta = game.Player1PointsDelta

				}

				drawMessage := Message{
					GameID:       int(game.ID),
					Type:         "draw",
					Board:        game.Board,
					Status:       "draw",
					Data:         message.Data,
					MyTime:       uint64(t1),
					OpponentTime: uint64(t2),
					MoveNotation: gameMove.Notation,
					MyInfo: PlayerInfo{
						UserName: player1.UserName,
						Rating:   strconv.FormatFloat(player1Rating.Rating, 'f', 0, 64),
					},
					OpponentInfo: PlayerInfo{
						UserName: player2.UserName,
						Rating:   strconv.FormatFloat(player2Rating.Rating, 'f', 0, 64),
					},

					MyPointsDelta:       myPointsDelta,
					OpponentPointsDelta: opponentPointsDelta,
				}

				msg, err := json.Marshal(drawMessage)

				if err != nil {

					log.Println("Invalid message")
					continue
				}

				if err := playerWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					playerWS.Conn.Close()
					removeSessionIfCurrent(playerID, ws)
				}

				drawMessage.MyTime = uint64(t2)
				drawMessage.OpponentTime = uint64(t1)

				drawMessage.MyPointsDelta = opponentPointsDelta
				drawMessage.OpponentPointsDelta = myPointsDelta

				drawMessage.MyInfo = PlayerInfo{
					UserName: player2.UserName,
					Rating:   strconv.FormatFloat(player2Rating.Rating, 'f', 0, 64),
				}

				drawMessage.OpponentInfo = PlayerInfo{
					UserName: player1.UserName,
					Rating:   strconv.FormatFloat(player1Rating.Rating, 'f', 0, 64),
				}

				msg, err = json.Marshal(drawMessage)

				if err != nil {

					log.Println("Invalid message")
					continue
				}

				if err := opponentWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					opponentWS.Conn.Close()
					removeSessionIfCurrent(opponentID, ws)
				}

				continue
			}

		}

		if opponentOk {
			message.CanKingSideCastle = opponentGameState.CanKingSideCastle
			message.CangLongCastle = opponentGameState.CanLongCastle

			message.MyTime = uint64(game.Player1RemainingTime)
			message.OpponentTime = uint64(game.Player2RemainingTime)

			if opponentID == game.Player2ID {
				message.MyTime = uint64(game.Player2RemainingTime)
				message.OpponentTime = uint64(game.Player1RemainingTime)
			}

			msg, err = json.Marshal(message)

			if err != nil {

				log.Println("Invalid message")
				continue
			}

			if err := opponentWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				opponentWS.Conn.Close()
				removeSessionIfCurrent(opponentID, ws)
			}
		}

		if playerOk {
			message.CanKingSideCastle = playerGameState.CanKingSideCastle
			message.CangLongCastle = playerGameState.CanLongCastle
			message.MyTime = uint64(game.Player1RemainingTime)
			message.OpponentTime = uint64(game.Player2RemainingTime)

			if playerID == game.Player2ID {
				message.MyTime = uint64(game.Player2RemainingTime)
				message.OpponentTime = uint64(game.Player1RemainingTime)
			}

			msg, err = json.Marshal(message)

			if err != nil {

				log.Println("Invalid message")
				continue
			}

			if err := playerWS.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				playerWS.Conn.Close()
				removeSessionIfCurrent(playerID, ws)
			}
		}

	}
}

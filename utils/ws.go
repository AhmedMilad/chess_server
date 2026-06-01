package utils

import (
	db "chess_server/database"
	"chess_server/models"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

var Players = make(map[uint]*websocket.Conn)
var Upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // Allow all connections
}

type Message struct {
	GameID            int             `json:"game_id"`
	Type              string          `json:"type"`
	Status            string          `json:"status"`
	Color             string          `json:"color"`
	Data              json.RawMessage `json:"data"`
	Board             string          `json:"board"`
	Turn              int             `json:"turn"`
	CangLongCastle    bool            `json:"can_long_castle"`
	CanKingSideCastle bool            `json:"can_king_side_castle"`
	EnpassantSquare   string          `json:"enpassant_square"`
	OpponentInfo      PlayerInfo      `json:"opponent_info"`
	MyInfo            PlayerInfo      `json:"my_info"`
	MyTime            uint64          `json:"my_time"`
	OpponentTime      uint64          `json:"opponent_time"`
	MoveNotation      string          `json:"move_notation"`
	Moves             []string        `json:"moves"`
	PromoteTo         string          `json:"promote_to"`
}

func HandleConnection(playerId uint, w http.ResponseWriter, r *http.Request) {
	ws, err := Upgrader.Upgrade(w, r, nil)
	defer func() {
		PlayerMutex.Lock()
		delete(Players, playerId)
		PlayerMutex.Unlock()
		ws.Close()
	}()
	if err != nil {
		return
	}
	defer ws.Close()

	PlayerMutex.Lock()
	Players[playerId] = ws
	PlayerMutex.Unlock()

	HandleSocketMessages(playerId, ws)

}

func HandleReConnection(playerID uint, gameId int, w http.ResponseWriter, r *http.Request) {
	ws, err := Upgrader.Upgrade(w, r, nil)
	defer func() {
		PlayerMutex.Lock()
		delete(Players, playerID)
		PlayerMutex.Unlock()
		ws.Close()
	}()
	if err != nil {
		return
	}
	defer ws.Close()
	PlayerMutex.Lock()

	delete(Players, playerID)
	Players[playerID] = ws

	PlayerMutex.Unlock()

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

	if playerID == game.Player2ID {

		myTime = game.Player2RemainingTime
		opponentTime = game.Player1RemainingTime
		opponentID = game.Player1ID
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

	var moves []string
	var playerRating models.UserGameRating
	var opponentRating models.UserGameRating

	db.DB.Model(&models.GameMove{}).Where("game_id = ?", game.ID).Order("id asc").Pluck("notation", &moves)
	db.DB.Preload("User").Where("user_id = ? AND game_type_id = ?", playerID, game.GameTypeID).First(&playerRating)
	db.DB.Preload("User").Where("user_id = ? AND game_type_id = ?", opponentID, game.GameTypeID).First(&opponentRating)

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
	opponentWS, ok := Players[playerID]
	PlayerMutex.Unlock()

	if ok {
		if err := opponentWS.WriteMessage(websocket.TextMessage, msg); err != nil {
			opponentWS.Close()
			PlayerMutex.Lock()
			delete(Players, playerID)
			PlayerMutex.Unlock()
		}
	}

	HandleSocketMessages(playerID, ws)

}

func HandleSocketMessages(playerID uint, ws *websocket.Conn) {
	for {
		_, msg, err := ws.ReadMessage()

		if err != nil {
			PlayerMutex.Lock()
			delete(Players, playerID)
			PlayerMutex.Unlock()
			break
		}

		var message Message

		if err := json.Unmarshal(msg, &message); err != nil {
			log.Println("Invalid message:", err)
			continue
		}
		var game models.Game

		if err := db.DB.First(&game, message.GameID).Error; err != nil {
			log.Println("Game not found:", err)
			continue
		}

		if game.Status != "ongoing" || game.Player1RemainingTime == 0 {

			PlayerMutex.Lock()
			playerWS, ok := Players[playerID]
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

				if err := playerWS.WriteMessage(websocket.TextMessage, msg); err != nil {
					playerWS.Close()
					PlayerMutex.Lock()
					delete(Players, playerID)
					PlayerMutex.Unlock()
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
			playerWS, ok := Players[currentPlayerID]
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

				if err := playerWS.WriteMessage(websocket.TextMessage, msg); err != nil {
					playerWS.Close()
					PlayerMutex.Lock()
					delete(Players, playerID)
					PlayerMutex.Unlock()
				}
			}

			continue
		}

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

		PlayerMutex.Lock()

		opponentWS, opponentOk := Players[opponentID]
		playerWS, playerOk := Players[currentPlayerID]

		PlayerMutex.Unlock()

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

				db.DB.Where("id = ?", playerID).First(&player1)
				db.DB.Where("id = ?", opponentID).First(&player2)

				err = updatePlayerRating(playerID, game, &player1Rating)

				if err != nil {
					log.Println(err)
					continue
				}

				err = updatePlayerRating(opponentID, game, &player2Rating)

				if err != nil {
					log.Println(err)
					continue
				}

				checkMateMessage := Message{
					GameID:       int(game.ID),
					Type:         "checkmate",
					Board:        game.Board,
					Status:       "win",
					Data:         message.Data,
					MyTime:       uint64(t1),
					OpponentTime: uint64(t2),
					MyInfo: PlayerInfo{
						UserName: player1.UserName,
						Rating:   strconv.FormatFloat(player1Rating.Rating, 'f', 0, 64),
					},
					OpponentInfo: PlayerInfo{
						UserName: player2.UserName,
						Rating:   strconv.FormatFloat(player2Rating.Rating, 'f', 0, 64),
					},
				}

				msg, err := json.Marshal(checkMateMessage)

				if err != nil {

					log.Println("Invalid message")
					continue
				}

				if err := playerWS.WriteMessage(websocket.TextMessage, msg); err != nil {
					PlayerMutex.Lock()
					playerWS.Close()
					delete(Players, playerID)
					PlayerMutex.Unlock()
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

				msg, err = json.Marshal(checkMateMessage)

				if err != nil {

					log.Println("Invalid message")
					continue
				}

				if err := opponentWS.WriteMessage(websocket.TextMessage, msg); err != nil {
					PlayerMutex.Lock()
					opponentWS.Close()
					delete(Players, opponentID)
					PlayerMutex.Unlock()
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

				db.DB.Where("id = ?", playerID).First(&player1)
				db.DB.Where("id = ?", opponentID).First(&player2)

				err = updatePlayerRating(playerID, game, &player1Rating)

				if err != nil {
					log.Println(err)
					continue
				}

				err = updatePlayerRating(opponentID, game, &player2Rating)

				if err != nil {
					log.Println(err)
					continue
				}

				drawMessage := Message{
					GameID:       int(game.ID),
					Type:         "draw",
					Board:        game.Board,
					Status:       "draw",
					Data:         message.Data,
					MyTime:       uint64(t1),
					OpponentTime: uint64(t2),
					MyInfo: PlayerInfo{
						UserName: player1.UserName,
						Rating:   strconv.FormatFloat(player1Rating.Rating, 'f', 0, 64),
					},
					OpponentInfo: PlayerInfo{
						UserName: player2.UserName,
						Rating:   strconv.FormatFloat(player2Rating.Rating, 'f', 0, 64),
					},
				}

				msg, err := json.Marshal(drawMessage)

				if err != nil {

					log.Println("Invalid message")
					continue
				}

				if err := playerWS.WriteMessage(websocket.TextMessage, msg); err != nil {
					PlayerMutex.Lock()
					playerWS.Close()
					delete(Players, playerID)
					PlayerMutex.Unlock()
				}

				drawMessage.MyTime = uint64(t2)
				drawMessage.OpponentTime = uint64(t1)

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

				if err := opponentWS.WriteMessage(websocket.TextMessage, msg); err != nil {
					PlayerMutex.Lock()
					opponentWS.Close()
					delete(Players, opponentID)
					PlayerMutex.Unlock()
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

			if err := opponentWS.WriteMessage(websocket.TextMessage, msg); err != nil {
				PlayerMutex.Lock()
				opponentWS.Close()
				delete(Players, opponentID)
				PlayerMutex.Unlock()
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

			if err := playerWS.WriteMessage(websocket.TextMessage, msg); err != nil {
				PlayerMutex.Lock()
				playerWS.Close()
				delete(Players, playerID)
				PlayerMutex.Unlock()
			}
		}

	}
}

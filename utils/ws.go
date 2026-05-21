package utils

import (
	db "chess_server/database"
	"chess_server/models"
	"encoding/json"
	"log"
	"net/http"

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
}

func HandleConnection(playerId uint, w http.ResponseWriter, r *http.Request) {
	ws, err := Upgrader.Upgrade(w, r, nil)
	defer func() {
		delete(Players, playerId)
		ws.Close()
	}()
	if err != nil {
		return
	}
	defer ws.Close()

	Players[playerId] = ws

	HandleSocketMessages(playerId, ws)

}

func HandleReConnection(playerId uint, gameId int, w http.ResponseWriter, r *http.Request) {
	ws, err := Upgrader.Upgrade(w, r, nil)
	defer func() {
		delete(Players, playerId)
		ws.Close()
	}()
	if err != nil {
		return
	}
	defer ws.Close()
	delete(Players, playerId)
	Players[playerId] = ws

	var game models.Game
	if err := db.DB.Preload("Player1").Preload("Player2").First(&game, gameId).Error; err != nil {
		log.Println("Game not found:", err)
		return
	}

	opponent := game.Player2
	if game.Player1.ID != playerId {
		opponent = game.Player1
	}

	var playerRating int
	for _, rating := range opponent.Ratings {
		if rating.GameTypeID == uint(game.GameTypeID) {
			playerRating = rating.Rating
		}
	}

	opponentPlayer := Player{
		UserID:     opponent.ID,
		GameTypeID: uint(game.GameTypeID),
		Rating:     playerRating,
	}

	message := NotificationMessage{
		Type:     "reconnect_game",
		GameId:   gameId,
		Opponent: opponentPlayer,
		IsBlack:  game.Player2ID == playerId,
		Board:    game.Board,
		Turn:     game.PlayerTurn,
	}

	targetPlayerID := game.Player2ID
	if game.PlayerTurn == 2 {
		targetPlayerID = game.Player1ID
	}
	gameState := models.GameState{}
	fetchErr := db.DB.Where("game_id = ? AND user_id = ?", game.ID, targetPlayerID).First(&gameState).Error
	if fetchErr == nil {
		message.EnpassantSquare = gameState.Enpassant
	}

	msg, err := json.Marshal(message)
	if err != nil {
		log.Println("Invalid message")
	}

	if opponentWS, ok := Players[playerId]; ok {
		if err := opponentWS.WriteMessage(websocket.TextMessage, msg); err != nil {
			opponentWS.Close()
			delete(Players, playerId)
		}
	}

	HandleSocketMessages(playerId, ws)

}

func HandleSocketMessages(playerID uint, ws *websocket.Conn) {
	for {
		_, msg, err := ws.ReadMessage()

		if err != nil {
			delete(Players, playerID)
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

		message.Board = game.Board
		message.Turn = game.PlayerTurn
		message.EnpassantSquare = opponentGameState.Enpassant
		message.Status = "failed"

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

		moveError := GenericHandleMove(&game, &playerGameState, &opponentGameState, &message)

		if moveError != nil {
			log.Println("Invalid Move Error: ", moveError)

			// send only to the player who made the action
			if playerWS, ok := Players[currentPlayerID]; ok {
				message.CanKingSideCastle = playerGameState.CanKingSideCastle
				message.CangLongCastle = playerGameState.CanLongCastle

				msg, err = json.Marshal(message)

				if err != nil {

					log.Println("Invalid message")
					continue
				}

				if err := playerWS.WriteMessage(websocket.TextMessage, msg); err != nil {
					playerWS.Close()
					delete(Players, opponentID)
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

		if opponentWS, ok := Players[opponentID]; ok {
			message.CanKingSideCastle = opponentGameState.CanKingSideCastle
			message.CangLongCastle = opponentGameState.CanLongCastle

			msg, err = json.Marshal(message)

			if err != nil {

				log.Println("Invalid message")
				continue
			}

			if err := opponentWS.WriteMessage(websocket.TextMessage, msg); err != nil {
				opponentWS.Close()
				delete(Players, opponentID)
			}
		}

		if playerWS, ok := Players[currentPlayerID]; ok {
			message.CanKingSideCastle = playerGameState.CanKingSideCastle
			message.CangLongCastle = playerGameState.CanLongCastle

			msg, err = json.Marshal(message)

			if err != nil {

				log.Println("Invalid message")
				continue
			}

			if err := playerWS.WriteMessage(websocket.TextMessage, msg); err != nil {
				playerWS.Close()
				delete(Players, opponentID)
			}
		}

	}
}

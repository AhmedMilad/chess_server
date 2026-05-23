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
	Opponent          Player          `json:"opponent"`
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

	message := Message{
		Type:   "reconnect_game",
		GameID: gameId,
		Board:  game.Board,
		Turn:   game.PlayerTurn,
		Color:  color,
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

		if game.Status != "ongoing" || game.Player1RemainingTime == 0 || game.Player1RemainingTime == 0 {

			PlayerMutex.Lock()
			playerWS, ok := Players[playerID]
			PlayerMutex.Unlock()

			if ok {

				status := "win"

				if game.WinnerID != &playerID {
					status = "defeat"
				}

				message.Type = "game_over"
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

		moveError := GenericHandleMove(&game, &playerGameState, &opponentGameState, &message)

		if moveError != nil {
			log.Println("Invalid Move Error: ", moveError)

			// send only to the player who made the action

			PlayerMutex.Lock()
			playerWS, ok := Players[currentPlayerID]
			PlayerMutex.Unlock()

			if ok {
				message.CanKingSideCastle = playerGameState.CanKingSideCastle
				message.CangLongCastle = playerGameState.CanLongCastle

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

		PlayerMutex.Lock()
		opponentWS, ok := Players[opponentID]
		PlayerMutex.Unlock()

		if ok {
			message.CanKingSideCastle = opponentGameState.CanKingSideCastle
			message.CangLongCastle = opponentGameState.CanLongCastle

			msg, err = json.Marshal(message)

			if err != nil {

				log.Println("Invalid message")
				continue
			}

			if err := opponentWS.WriteMessage(websocket.TextMessage, msg); err != nil {
				opponentWS.Close()
				PlayerMutex.Lock()
				delete(Players, opponentID)
				PlayerMutex.Unlock()
			}
		}

		PlayerMutex.Lock()
		playerWS, ok := Players[currentPlayerID]
		PlayerMutex.Unlock()

		if ok {
			message.CanKingSideCastle = playerGameState.CanKingSideCastle
			message.CangLongCastle = playerGameState.CanLongCastle

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

	}
}

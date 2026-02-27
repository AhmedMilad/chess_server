package utils

import (
	db "chess_server/database"
	"chess_server/models"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/websocket"
)

var Players = make(map[uint]*websocket.Conn)
var Upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // Allow all connections
}

type Message struct {
	GameID int             `json:"game_id"`
	Type   string          `json:"type"`
	Data   json.RawMessage `json:"data"`
	Board  string          `json:"board"`
	Turn   int             `json:"turn"`
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
		fmt.Println("Game not found:", err)
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
	msg, err := json.Marshal(message)
	if err != nil {
		fmt.Println("Invalid message")
	}

	if opponentWS, ok := Players[playerId]; ok {
		if err := opponentWS.WriteMessage(websocket.TextMessage, msg); err != nil {
			opponentWS.Close()
			delete(Players, playerId)
		}
	}

	HandleSocketMessages(playerId, ws)

}

func HandleSocketMessages(playerId uint, ws *websocket.Conn) {
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			delete(Players, playerId)
			break
		}

		var message Message
		if err := json.Unmarshal(msg, &message); err != nil {
			fmt.Println("Invalid message:", err)
			continue
		}
		var game models.Game
		if err := db.DB.First(&game, message.GameID).Error; err != nil {
			fmt.Println("Game not found:", err)
			continue
		}
		var moveData struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		if message.Type == "move" {
			if err := json.Unmarshal(message.Data, &moveData); err != nil {
				fmt.Println("Error unmarshaling move data:", err)
				return
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
				fmt.Println("Invalid board notation:", err)
				return
			}

			fmt.Println(moveData)

			fromIndex, err := getMoveNotationIndex(moveData.From)
			if err != nil {
				fmt.Println("Invalid move from:", err)
				return
			}

			toIndex, err := getMoveNotationIndex(moveData.To)
			if err != nil {
				fmt.Println("Invalid move to:", err)
				return
			}

			currentSquare := board[(*fromIndex)[0]][(*fromIndex)[1]]
			board[(*fromIndex)[0]][(*fromIndex)[1]] = " "
			board[(*toIndex)[0]][(*toIndex)[1]] = currentSquare

			newBoardNotation, err := GetFenNotation(*board)
			if err != nil {
				fmt.Println("Invalid board notation:", err)
				return
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
			var opponentID uint
			if game.Player1ID == playerId {
				opponentID = game.Player2ID
			} else if game.Player2ID == playerId {
				opponentID = game.Player1ID
			} else {
				fmt.Println("Player not part of this game")
				continue
			}
			message.Board = *newBoardNotation
			message.Turn = game.PlayerTurn
			msg, err = json.Marshal(message)
			if err != nil {
				fmt.Println("Invalid message")
			}
			if opponentWS, ok := Players[opponentID]; ok {
				if err := opponentWS.WriteMessage(websocket.TextMessage, msg); err != nil {
					opponentWS.Close()
					delete(Players, opponentID)
				}
			}
		}

	}
}

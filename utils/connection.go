package utils

import (
	"chess_server/database"
	"chess_server/models"
	"context"
	"encoding/json"
	"log"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

type ConnMessage struct {
	Type string `json:"type"`
}

const (
	pongWait   = 25 * time.Second
	pingPeriod = (pongWait * 9) / 10 // send every 18 seconds
)

func CheckConnection() {

	var games []models.Game

	if err := db.DB.Where("status = ?", "ongoing").Find(&games).Error; err != nil {
		log.Println("Invalid message")
		return
	}

	for _, game := range games {

		p1ID := game.Player1ID
		p2ID := game.Player2ID

		PlayerMutex.RLock()
		p1Session, ok1 := Sessions[p1ID]
		p2Session, ok2 := Sessions[p2ID]
		PlayerMutex.RUnlock()

		if !ok1 {
			continue
		}

		if !ok2 {
			continue
		}

		now := time.Now()
		p1Duration := now.Sub(p1Session.LastSeen)
		p2Duration := now.Sub(p2Session.LastSeen)

		const disconnectWarnAfter = 30 * time.Second
		const disconnectTimeoutAfter = 60 * time.Second

		if p1Duration >= disconnectWarnAfter && p1Duration < disconnectTimeoutAfter {

			if !p1Session.IsDisconnected {
				p1Session.IsDisconnected = true

				PlayerMutex.Lock()
				Sessions[p1ID] = p1Session
				PlayerMutex.Unlock()

				sendMessage(ConnMessage{
					Type: "opponent_disconnected",
				}, p2ID, p2Session)
			}

		}

		if p2Duration >= disconnectWarnAfter && p2Duration < disconnectTimeoutAfter {

			if !p2Session.IsDisconnected {
				p2Session.IsDisconnected = true

				PlayerMutex.Lock()
				Sessions[p2ID] = p2Session
				PlayerMutex.Unlock()

				sendMessage(ConnMessage{
					Type: "opponent_disconnected",
				}, p1ID, p1Session)
			}

		}

		if p1Duration >= disconnectTimeoutAfter {
			HandleDisconnectionTimeout(game, p2ID, p1ID)
			continue
		}

		if p2Duration >= disconnectTimeoutAfter {
			HandleDisconnectionTimeout(game, p1ID, p2ID)
			continue
		}

	}
}

func sendMessage(message ConnMessage, pID uint, session Session) error {

	msg, err := json.Marshal(message)

	if err != nil {
		log.Println("Invalid message")
		return err
	}

	if err := session.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
		session.Conn.Close()
		PlayerMutex.Lock()
		delete(Sessions, pID)
		PlayerMutex.Unlock()

		return err
	}

	return nil
}

func HandleDisconnectionTimeout(game models.Game, wID, lID uint) {

	wTime := game.Player1RemainingTime
	lTime := game.Player2RemainingTime

	if wID == game.Player2ID {

		wTime = game.Player2RemainingTime
		lTime = game.Player1RemainingTime
	}

	PlayerMutex.Lock()
	wWS, ok1 := Sessions[wID]
	lWS, ok2 := Sessions[lID]
	PlayerMutex.Unlock()

	if !ok1 {
		log.Printf("Could not get the connection of the player id: %v", wID)
		return
	}

	if !ok2 {
		log.Printf("Could not get the connection of the player id: %v", lID)
		return
	}

	var winnerRating models.UserGameRating
	var loserRating models.UserGameRating

	if err := db.DB.Where("user_id = ? AND game_type_id = ?", wID, game.GameTypeID).First(&winnerRating).Error; err != nil {

		log.Printf("Could not find ther user game rating with the user id %d and game type id %d while creating a game", wID, game.GameTypeID)
		return
	}

	if err := db.DB.Where("user_id = ? AND game_type_id = ?", lID, game.GameTypeID).First(&loserRating).Error; err != nil {

		log.Printf("Could not find ther user game rating with the user id %d and game type id %d while creating a game", lID, game.GameTypeID)
		return
	}

	err := db.DB.Transaction(func(tx *gorm.DB) error {

		game.Status = "finished"
		game.WinnerID = &wID

		err := updatePlayerRating(wID, &game, &winnerRating, tx)

		if err != nil {
			log.Println(err)

			return err
		}

		err = updatePlayerRating(lID, &game, &loserRating, tx)

		if err != nil {
			log.Println(err)
			return err
		}

		return nil
	})

	if err != nil {
		log.Println("Could not update players rating")
		return
	}

	var wPlayer models.User
	var lPlayer models.User

	myPointsDelta := game.Player1PointsDelta
	opponentPointsDelta := game.Player2PointsDelta

	if wID == game.Player2ID {
		myPointsDelta = game.Player2PointsDelta
		opponentPointsDelta = game.Player1PointsDelta

	}

	if err := db.DB.Where("id = ?", wID).First(&wPlayer).Error; err != nil {
		log.Printf("could not fetch a player with the ID: %v", wID)
		return
	}

	if err := db.DB.Where("id = ?", lID).First(&lPlayer).Error; err != nil {
		log.Printf("could not fetch a player with the ID: %v", lID)
		return
	}

	message := Message{
		GameID:       int(game.ID),
		Type:         "time_out",
		Status:       "win",
		Board:        game.Board,
		MyTime:       uint64(wTime),
		OpponentTime: uint64(lTime),
		MyInfo: PlayerInfo{
			UserName: wPlayer.UserName,
			Rating:   strconv.FormatFloat(winnerRating.Rating, 'f', 0, 64),
		},
		OpponentInfo: PlayerInfo{
			UserName: lPlayer.UserName,
			Rating:   strconv.FormatFloat(loserRating.Rating, 'f', 0, 64),
		},
		MyPointsDelta:       myPointsDelta,
		OpponentPointsDelta: opponentPointsDelta,
	}
	msg1, err := json.Marshal(message)

	if err != nil {
		log.Println("Invalid message")

		return
	}

	if wWS.Conn != nil {
		if err := wWS.Conn.WriteMessage(websocket.TextMessage, msg1); err != nil {
			wWS.Conn.Close()
			PlayerMutex.Lock()
			delete(Sessions, wID)
			PlayerMutex.Unlock()
		}
	}

	message.Status = "defeat"
	message.MyTime = uint64(lTime)
	message.OpponentTime = uint64(wTime)

	message.MyPointsDelta = opponentPointsDelta
	message.OpponentPointsDelta = myPointsDelta

	message.MyInfo = PlayerInfo{
		UserName: lPlayer.UserName,
		Rating:   strconv.FormatFloat(winnerRating.Rating, 'f', 0, 64),
	}

	message.OpponentInfo = PlayerInfo{
		UserName: wPlayer.UserName,
		Rating:   strconv.FormatFloat(loserRating.Rating, 'f', 0, 64),
	}
	msg2, err := json.Marshal(message)

	if err != nil {
		log.Println("Invalid message")

		return
	}

	if lWS.Conn != nil {
		if err := lWS.Conn.WriteMessage(websocket.TextMessage, msg2); err != nil {
			lWS.Conn.Close()
			PlayerMutex.Lock()
			delete(Sessions, lID)
			PlayerMutex.Unlock()
		}
	}

}

func HandlePongMessage(pID uint) {
	PlayerMutex.Lock()
	defer PlayerMutex.Unlock()

	session, ok := Sessions[pID]
	if !ok {
		return
	}

	session.LastSeen = time.Now()
	session.IsDisconnected = false
	Sessions[pID] = session
}

func StartConnectionChecker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Println("connection checker started, interval:", interval)

	for {
		select {
		case <-ctx.Done():
			log.Println("connection checker stopping")
			return

		case <-ticker.C:
			safeCheckConnection()
		}
	}
}

func safeCheckConnection() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("recovered from panic in CheckConnection: %v", r)
		}
	}()

	CheckConnection()
}

func RegisterConnection(pID uint, conn *websocket.Conn) {
	conn.SetReadDeadline(time.Now().Add(pongWait))

	conn.SetPongHandler(func(appData string) error {
		log.Printf("pong received from player %d at %v", pID, time.Now())
		conn.SetReadDeadline(time.Now().Add(pongWait))

		PlayerMutex.Lock()
		if session, ok := Sessions[pID]; ok {
			session.LastSeen = time.Now()
			session.IsDisconnected = false
			Sessions[pID] = session
		}
		PlayerMutex.Unlock()

		return nil
	})

	PlayerMutex.Lock()
	Sessions[pID] = Session{
		Conn:     conn,
		LastSeen: time.Now(),
	}
	PlayerMutex.Unlock()

	go pingLoop(pID, conn)
}

func pingLoop(pID uint, conn *websocket.Conn) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	if !sendPing(pID, conn) {
		return
	}

	for range ticker.C {
		if !sendPing(pID, conn) {
			return
		}
	}
}

func sendPing(pID uint, conn *websocket.Conn) bool {
	PlayerMutex.RLock()
	session, ok := Sessions[pID]
	PlayerMutex.RUnlock()

	if !ok || session.Conn != conn {
		return false
	}

	if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
		log.Printf("ping failed for player %d: %v", pID, err)
		conn.Close()
		removeSessionIfCurrent(pID, conn)
		return false
	}

	return true
}

func removeSessionIfCurrent(pID uint, conn *websocket.Conn) {
	PlayerMutex.Lock()
	if s, ok := Sessions[pID]; ok && s.Conn == conn {
		delete(Sessions, pID)
	}
	PlayerMutex.Unlock()
}

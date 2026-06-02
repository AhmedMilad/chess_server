package utils

import (
	"chess_server/config"
	"chess_server/database"
	"chess_server/models"
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"encoding/json"

	"github.com/go-redis/redis/v8"
)

var RDB *redis.Client
var PUBSUB *redis.PubSub
var ActiveTrackers sync.Map
var PlayerMutex sync.RWMutex

func InitRedis() {
	RDB = redis.NewClient(&redis.Options{
		Addr:     config.Config.RedisAddr,
		Password: config.Config.RedisPass,
		DB:       config.Config.RedisDB,
	})

	if err := RDB.Ping(Ctx).Err(); err != nil {
		log.Fatalf("Could not connect to Redis: %v", err)
	}

	if err := RDB.ConfigSet(Ctx, "notify-keyspace-events", "Ex").Err(); err != nil {
		log.Printf("Warning: Could not set notify-keyspace-events: %v. Ensure your Redis user has CONFIG permissions.", err)
	} else {
		log.Println("Redis notify-keyspace-events successfully set to 'Ex'")
	}

	if config.Config.Debug {
		if err := RDB.Del(Ctx, "players_q", "players_q_set").Err(); err != nil {
			log.Printf("Failed to clear player queues: %v", err)
		} else {
			log.Println("Redis queues 'players_q' and 'players_q_set' cleared (debug mode)")
		}
	}

}

func TrackWatches() {
	pubsub := RDB.PSubscribe(Ctx, "__keyevent@0__:expired", "__keyevent@0__:del")
	ch := pubsub.Channel()

	go func() {
		defer pubsub.Close()

		for msg := range ch {
			key := msg.Payload

			if _, exists := ActiveTrackers.Load(key); exists {

				func(key string) {

					defer ActiveTrackers.Delete(key)

					game := models.Game{}

					if err := db.DB.Where("id = ? AND status = 'ongoing'", key).First(&game).Error; err != nil {
						log.Printf("failed to find game %s in db: %v", key, err)
						return
					}

					wID := game.Player1ID
					lID := game.Player2ID

					pID := game.Player1ID

					if game.PlayerTurn == 2 {
						pID = game.Player2ID
					}

					winnerTime := game.Player1RemainingTime

					if wID == uint(pID) {
						lID = game.Player1ID
						wID = game.Player2ID

						game.Player1RemainingTime = 0
						winnerTime = game.Player2RemainingTime

					} else {
						game.Player2RemainingTime = 0

					}

					game.Status = "finished"
					game.WinnerID = &wID

					if err := db.DB.Save(&game).Error; err != nil {
						log.Println(err.Error())

						return
					}

					PlayerMutex.Lock()
					wWS := Players[wID]
					lWS := Players[lID]
					PlayerMutex.Unlock()

					var player1Rating models.UserGameRating
					var player2Rating models.UserGameRating

					if err := db.DB.Where("user_id = ? AND game_type_id = ?", wID, game.GameTypeID).First(&player1Rating).Error; err != nil {

						log.Printf("Could not find ther user game rating with the user id %d and game type id %d while creating a game", wID, game.GameTypeID)
						return
					}

					if err := db.DB.Where("user_id = ? AND game_type_id = ?", lID, game.GameTypeID).First(&player2Rating).Error; err != nil {

						log.Printf("Could not find ther user game rating with the user id %d and game type id %d while creating a game", lID, game.GameTypeID)
						return
					}

					err := updatePlayerRating(wID, &game, &player1Rating)

					if err != nil {
						log.Println(err)

						return
					}

					err = updatePlayerRating(lID, &game, &player2Rating)

					if err != nil {
						log.Println(err)
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

					db.DB.Where("id = ?", wID).First(&wPlayer)
					db.DB.Where("id = ?", lID).First(&lPlayer)

					message := Message{
						GameID:       int(game.ID),
						Type:         "time_out",
						Status:       "win",
						Board:        game.Board,
						MyTime:       uint64(winnerTime),
						OpponentTime: 0,
						MyInfo: PlayerInfo{
							UserName: wPlayer.UserName,
							Rating:   strconv.FormatFloat(player1Rating.Rating, 'f', 0, 64),
						},
						OpponentInfo: PlayerInfo{
							UserName: lPlayer.UserName,
							Rating:   strconv.FormatFloat(player2Rating.Rating, 'f', 0, 64),
						},
						MyPointsDelta:       myPointsDelta,
						OpponentPointsDelta: opponentPointsDelta,
					}
					msg1, err := json.Marshal(message)

					if err != nil {
						log.Println("Invalid message")

						return
					}

					if wWS != nil {
						if err := wWS.WriteMessage(websocket.TextMessage, msg1); err != nil {
							wWS.Close()
							PlayerMutex.Lock()
							delete(Players, wID)
							PlayerMutex.Unlock()
						}
					}

					message.Status = "defeat"
					message.MyTime = 0
					message.OpponentTime = uint64(winnerTime)

					message.MyPointsDelta = opponentPointsDelta
					message.OpponentPointsDelta = myPointsDelta

					message.MyInfo = PlayerInfo{
						UserName: lPlayer.UserName,
						Rating:   strconv.FormatFloat(player2Rating.Rating, 'f', 0, 64),
					}

					message.OpponentInfo = PlayerInfo{
						UserName: wPlayer.UserName,
						Rating:   strconv.FormatFloat(player1Rating.Rating, 'f', 0, 64),
					}
					msg2, err := json.Marshal(message)

					if err != nil {
						log.Println("Invalid message")

						return
					}

					if lWS != nil {
						if err := lWS.WriteMessage(websocket.TextMessage, msg2); err != nil {
							lWS.Close()
							PlayerMutex.Lock()
							delete(Players, lID)
							PlayerMutex.Unlock()
						}
					}

					log.Printf("[Event] Key '%s' triggered action'\n", key)
				}(key)
			}
		}
	}()
}

func AddWatch(key uint, ttl time.Duration) {

	keyStr := strconv.FormatUint(uint64(key), 10)

	ActiveTrackers.Store(keyStr, true)

	err := RDB.Set(Ctx, keyStr, "active", ttl).Err()
	if err != nil {
		log.Printf("Error setting key: %v\n", err)
		ActiveTrackers.Delete(keyStr)
	}
}

func UpdateWatch(key uint, newTTL time.Duration) error {

	keyStr := strconv.FormatUint(uint64(key), 10)

	success, err := RDB.Expire(Ctx, keyStr, newTTL).Result()
	if err != nil {
		log.Printf("Error updating TTL for key '%d': %v\n", key, err)
		return err
	}

	if success {

		ActiveTrackers.Store(keyStr, true)
		log.Printf("Successfully updated key '%d' with a new TTL of %v\n", key, newTTL)
	} else {

		message := fmt.Sprintf("Failed to update TTL: Key '%d' does not exist in Redis.\n", key)

		log.Println(message)
		ActiveTrackers.Delete(keyStr)

		return errors.New(message)
	}

	return nil

}

func RemoveWatch(key string) {

	ActiveTrackers.Delete(key)
	RDB.Del(Ctx, key)
}

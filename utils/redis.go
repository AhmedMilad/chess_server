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

					if err := db.DB.Where("id = ?", key).First(&game).Error; err != nil {
						log.Printf("failed to find game %s in db: %v", key, err)
						return
					}

					wID := game.Player1ID
					lID := game.Player2ID

					pID := game.Player1ID

					if game.PlayerTurn == 2 {
						pID = game.Player2ID
					}

					if wID == uint(pID) {
						lID = game.Player1ID
						wID = game.Player2ID
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

					message := Message{
						GameID: int(game.ID),
						Type:   "game_over",
						Status: "win",
						Board:  game.Board,
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

					log.Printf("[Event] Key '%s' triggered action via channel '%s'\n", key)
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
		log.Printf("Error updating TTL for key '%s': %v\n", key, err)
		return err
	}

	if success {

		ActiveTrackers.Store(keyStr, true)
		log.Printf("Successfully updated key '%s' with a new TTL of %v\n", key, newTTL)
	} else {

		message := fmt.Sprint("Failed to update TTL: Key '%s' does not exist in Redis.\n", key)

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

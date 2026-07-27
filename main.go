package main

import (
	"chess_server/config"
	"chess_server/database"
	"chess_server/routes"
	"context"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"chess_server/utils"
	"log"
	"os"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, using system environment variables")
	}
	config.LoadConfig()
	db.Init()
	utils.InitGame()
	utils.InitRedis()
	utils.TrackWatches()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go utils.MatchmakingWorker()
	go utils.StartConnectionChecker(ctx, 5*time.Second)

	router := gin.Default()

	router.Use(cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
			return origin == config.Config.Client
		},
		AllowMethods:     []string{"POST", "GET", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	routes.ApiRoutes(router)
	router.Run(os.Getenv("DOMAIN"))
}

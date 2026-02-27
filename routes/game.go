package routes

import (
	"chess_server/controllers"
	"github.com/gin-gonic/gin"
)

func GameRoutes(api *gin.RouterGroup) {
	gameRoutes := api.Group("/games")
	gameRoutes.GET("/", controllers.GetGameTypes)
	gameRoutes.GET("/:id/play", controllers.PlayGame)
	gameRoutes.GET("/:id/reconnect", controllers.ReConnect)
}

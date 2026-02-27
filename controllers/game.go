package controllers

import (
	"chess_server/database"
	"chess_server/models"
	"net/http"

	"chess_server/utils"
	"strconv"

	"github.com/gin-gonic/gin"
)

type TestWSRequest struct {
	Message string `json:"message" binding:"required,omitempty"`
}

func GetGameTypes(c *gin.Context) {
	var gameTypes []models.GameType
	result := db.DB.Find(&gameTypes)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Error retrieving game types"})
		return
	}

	type GameTypeResponse struct {
		ID       uint   `json:"id"`
		Name     string `json:"name"`
		Duration uint   `json:"duration"`
	}

	responses := make([]GameTypeResponse, 0, len(gameTypes))
	for _, gt := range gameTypes {
		responses = append(responses, GameTypeResponse{
			ID:       gt.ID,
			Name:     gt.Name,
			Duration: gt.Duration,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "game types",
		"data":    responses,
	})
}

func PlayGame(c *gin.Context) {
	token := c.Query("token")
	claims, err := utils.ValidateToken(token)

	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token", "details": err.Error()})
		c.Abort()
		return
	}
	var user models.User
	result := db.DB.First(&user, claims["id"])
	if result.Error != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
		c.Abort()
		return
	}

	gameTypeId, _ := strconv.Atoi(c.Param("id"))
	utils.EnqueuePlayer(user.ID, gameTypeId)

	utils.HandleConnection(user.ID, c.Writer, c.Request)
}

func ReConnect(c *gin.Context) {
	token := c.Query("token")
	claims, err := utils.ValidateToken(token)

	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token", "details": err.Error()})
		c.Abort()
		return
	}
	var user models.User
	result := db.DB.First(&user, claims["id"])
	if result.Error != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
		c.Abort()
		return
	}
	gameId, _ := strconv.Atoi(c.Param("id"))

	utils.HandleReConnection(user.ID, gameId, c.Writer, c.Request)
}

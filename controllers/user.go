package controllers

import (
	"chess_server/database"
	"chess_server/models"
	"errors"
	"gorm.io/gorm"
	"net/http"

	"github.com/gin-gonic/gin"
)

type GameRatingResponse struct {
	GameType string `json:"game_type"`
	Duration uint   `json:"duration"`
	Rating   int    `json:"rating"`
}

type SettingResponse struct {
	BoardTheme    string `json:"board_theme"`
	SystemMode    string `json:"system_mode"`
	PieceStyle    string `json:"piece_style"`
	Notifications bool   `json:"notifications"`
}

type UserInfoResponse struct {
	ID       uint                 `json:"id"`
	UserName string               `json:"username"`
	Email    string               `json:"email"`
	Settings SettingResponse      `json:"settings"`
	Ratings  []GameRatingResponse `json:"ratings"`
}

type ListUserResponse struct {
	ID       uint                 `json:"id"`
	UserName string               `json:"username"`
	Email    string               `json:"email"`
	Ratings  []GameRatingResponse `json:"ratings"`
}

type UpdateUserRequest struct {
	UserName string `json:"username" binding:"required,omitempty,min=3"`
	Email    string `json:"email" binding:"required,omitempty,email"`
}

type UpdateSettingRequest struct {
	BoardTheme    string `json:"board_theme" binding:"required,omitempty"`
	SystemMode    string `json:"system_mode" binding:"required,omitempty"`
	PieceStyle    string `json:"piece_style" binding:"required,omitempty"`
	Notifications *bool  `json:"notifications" binding:"required,omitempty"`
}

func GetSettings(c *gin.Context) {
	userData, ok := c.Get("user")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Could not get the user"})
		return
	}

	user, ok := userData.(models.User)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Could not get the user"})
		return
	}

	var settings models.Setting
	if err := db.DB.Where("user_id = ?", user.ID).First(&settings).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": "Settings not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Database error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "user settings",
		"data": gin.H{
			"BoardTheme":    settings.BoardTheme,
			"SystemMode":    settings.SystemMode,
			"PieceStyle":    settings.PieceStyle,
			"Notifications": settings.Notifications,
		},
	})
}

func GetInfo(c *gin.Context) {
	userData, ok := c.Get("user")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Could not get the user"})
		return
	}

	user, ok := userData.(models.User)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Could not get the user"})
		return
	}

	var dbUser models.User
	if err := db.DB.
		Preload("Setting").
		Preload("Ratings.GameType").
		First(&dbUser, user.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "User not found"})
		return
	}

	resp := UserInfoResponse{
		ID:       dbUser.ID,
		UserName: dbUser.UserName,
		Email:    dbUser.Email,
		Settings: SettingResponse{
			BoardTheme:    dbUser.Setting.BoardTheme,
			SystemMode:    dbUser.Setting.SystemMode,
			PieceStyle:    dbUser.Setting.PieceStyle,
			Notifications: dbUser.Setting.Notifications,
		},
	}

	for _, r := range dbUser.Ratings {
		resp.Ratings = append(resp.Ratings, GameRatingResponse{
			GameType: r.GameType.Name,
			Duration: r.GameType.Duration,
			Rating:   int(r.Rating),
		})
	}

	c.JSON(http.StatusOK, resp)
}

func ListUsers(c *gin.Context) {
	var users []models.User

	if err := db.DB.
		Preload("Ratings.GameType").
		Preload("Setting").
		Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Could not fetch users"})
		return
	}

	var response []ListUserResponse
	for _, u := range users {
		r := ListUserResponse{
			ID:       u.ID,
			UserName: u.UserName,
			Email:    u.Email,
		}

		for _, rating := range u.Ratings {
			r.Ratings = append(r.Ratings, GameRatingResponse{
				GameType: rating.GameType.Name,
				Duration: rating.GameType.Duration,
				Rating:   int(rating.Rating),
			})
		}
		response = append(response, r)
	}

	c.JSON(http.StatusOK, response)
}

func UpdateUser(c *gin.Context) {
	userData, ok := c.Get("user")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Could not get the user"})
		return
	}

	user, ok := userData.(models.User)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Could not get the user"})
		return
	}

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request", "error": err.Error()})
		return
	}

	if err := db.DB.First(&user, user.ID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "User not found"})
		return
	}

	if req.UserName != "" {
		user.UserName = req.UserName
	}
	if req.Email != "" {
		user.Email = req.Email
	}

	if err := db.DB.Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Could not update user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User updated successfully"})
}

func UpdateUserSettings(c *gin.Context) {
	userData, ok := c.Get("user")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Could not get the user"})
		return
	}

	user, ok := userData.(models.User)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Could not get the user"})
		return
	}

	var req UpdateSettingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request", "error": err.Error()})
		return
	}

	var setting models.Setting
	if err := db.DB.Where("user_id = ?", user.ID).First(&setting).Error; err != nil {
		setting = models.Setting{UserID: user.ID}
		db.DB.Create(&setting)
	}

	if req.BoardTheme != "" {
		setting.BoardTheme = req.BoardTheme
	}
	if req.SystemMode != "" {
		setting.SystemMode = req.SystemMode
	}
	if req.PieceStyle != "" {
		setting.PieceStyle = req.PieceStyle
	}
	if req.Notifications != nil {
		setting.Notifications = *req.Notifications
	}

	if err := db.DB.Save(&setting).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Could not update settings"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Settings updated successfully"})
}

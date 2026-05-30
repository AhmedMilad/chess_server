package controllers

import (
	"chess_server/database"
	"chess_server/models"
	"chess_server/utils"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func Register(c *gin.Context) {
	var payload struct {
		Email    string `form:"email" json:"email" xml:"email"  binding:"required,email"`
		UserName string `form:"username" json:"username" xml:"username"  binding:"required"`
		Password string `form:"password" json:"password" xml:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "Error",
			"data":    err.Error(),
		})
		return
	}
	var existingUser models.User
	err := db.DB.Where("username = ? OR email = ?", payload.UserName, payload.Email).
		First(&existingUser).Error

	if err == nil {
		if existingUser.UserName == payload.UserName {
			c.JSON(http.StatusConflict, gin.H{
				"message": "Error",
				"data":    "Username already exists",
			})
			return
		}
		if existingUser.Email == payload.Email {
			c.JSON(http.StatusConflict, gin.H{
				"message": "Error",
				"data":    "Email already exists",
			})
			return
		}
	}
	hash, hashErr := utils.GenerateFromPassword(payload.Password)
	if hashErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "Error",
			"data":    "Something went wrong please try again later",
		})
		return
	}
	newUser := models.User{
		UserName: payload.UserName,
		Email:    payload.Email,
		Password: hash,
	}
	if err := db.DB.Create(&newUser).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "Error",
			"data":    "Failed to create the user",
		})
		return
	}

	defaultSetting := models.Setting{
		UserID:        newUser.ID,
		BoardTheme:    "standard",
		SystemMode:    "light",
		PieceStyle:    "standard",
		Notifications: true,
	}

	if err := db.DB.Create(&defaultSetting).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "Error",
			"data":    "Failed to create the settings",
		})
		return
	}

	var gameTypes []models.GameType
	db.DB.Find(&gameTypes)

	var ratings []models.UserGameRating
	for _, g := range gameTypes {
		ratings = append(ratings, models.UserGameRating{
			UserID:     newUser.ID,
			GameTypeID: g.ID,
			Rating:     1200,
		})
	}
	db.DB.Create(&ratings)

	token, createTokenErr := utils.CreateToken(
		map[string]interface{}{
			"id":       newUser.ID,
			"username": newUser.UserName,
		},
		time.Hour*24*30,
	)
	if createTokenErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "Error",
			"data":    "Something went wrong please try again later",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "Registered successfully",
		"data":    token,
	})
}

func Login(c *gin.Context) {
	var payload struct {
		UserName string `form:"username" json:"username" xml:"username"  binding:"required"`
		Password string `form:"password" json:"password" xml:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "Error",
			"data":    err.Error(),
		})
		return
	}

	var user models.User
	err := db.DB.Where("username = ?", payload.UserName).First(&user).Error

	if err != nil {
		c.JSON(http.StatusConflict, gin.H{
			"message": "Error",
			"data":    "There is no user with this username",
		})
		return
	}
	match, compareErr := utils.ComparePasswordAndHash(payload.Password, user.Password)
	if compareErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "Error",
			"data":    "Something went wrong please try again later",
		})
		return
	}
	if !match {
		c.JSON(http.StatusUnauthorized, gin.H{
			"message": "Error",
			"data":    "Incorrect password",
		})
		return
	}

	token, createTokenErr := utils.CreateToken(
		map[string]interface{}{
			"id":       user.ID,
			"username": user.UserName,
		},
		time.Hour*24*30,
	)
	if createTokenErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "Error",
			"data":    "Something went wrong please try again later",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "Logged in successfully",
		"data":    token,
	})
}

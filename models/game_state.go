package models

import (
	"time"
)

/**
* What would the game state should have??
* 1- can (king side/long) castle for each player.
* 2- which pawn can be taken as enpassant.
 */
type GameState struct {
	ID                uint   `gorm:"primaryKey;autoIncrement"`
	UserID            uint   `gorm:"not null;index"`
	GameID            uint   `gorm:"not null;index"`
	User              User   `gorm:"foreignKey:UserID"`
	Game              Game   `gorm:"foreignKey:GameID"`
	Enpassant         string `gorm:"type:varchar(4)"`
	CanLongCastle     bool   `gorm:"default:true"`
	CanKingSideCastle bool   `gorm:"default:true"`
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
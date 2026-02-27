package models

import (
	"gorm.io/datatypes"
	"time"
)

type Game struct {
	ID             uint           `gorm:"primaryKey;autoIncrement"`
	Player1ID      uint           `gorm:"not null"`
	Player2ID      uint           `gorm:"not null"`
	Player1        User           `gorm:"foreignKey:Player1ID"`
	Player2        User           `gorm:"foreignKey:Player2ID"`
	GameTypeID     uint           `gorm:"not null"`
	GameType       GameType       `gorm:"foreignKey:GameTypeID"`
	Status         string         `gorm:"type:varchar(50);default:'pending'"`
	WinnerID       *uint          `gorm:"default:null"`
	Moves          datatypes.JSON `gorm:"type:json"`
	Board          string         `gorm:"size:255"`
	PlayerTurn     int            `gorm:"check:player_turn IN (1,2)"`
	PointsAwarded  int            `gorm:"default:0"`
	PointsDeducted int            `gorm:"default:0"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

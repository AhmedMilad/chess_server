package models

import (
	"time"
)

type GameMove struct {
	ID              uint    `gorm:"primaryKey;autoIncrement"`
	GameID          uint    `gorm:"not null;index:idx_game_board"`
	Game            Game    `gorm:"foreignKey:GameID"`
	PlayerID        uint    `gorm:"not null"`
	Player          User    `gorm:"foreignKey:PlayerID"`
	From            string  `gorm:"type:varchar(50);default:'pending'"`
	To              string  `gorm:"type:varchar(50);default:'pending'"`
	Board           string  `gorm:"size:255;index:idx_game_board"`
	MoveTime        int64   `gorm:"not null"`
	Notation        string  `gorm:"size:14;not null"`
	EnpassantSquare *string `gorm:"size:12"`
	CastlingStatus  *string `gorm:"size:12"`
	MoveClass       *string `gorm:"type:varchar(20);default:null"`
	CentiPawn       *float64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

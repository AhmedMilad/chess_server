package models

import (
	"gorm.io/gorm"
	"time"
)

type Game struct {
	ID                     uint     `gorm:"primaryKey;autoIncrement"`
	Player1ID              uint     `gorm:"not null"`
	Player2ID              uint     `gorm:"not null"`
	Player1                User     `gorm:"foreignKey:Player1ID"`
	Player2                User     `gorm:"foreignKey:Player2ID"`
	GameTypeID             uint     `gorm:"not null"`
	GameType               GameType `gorm:"foreignKey:GameTypeID"`
	Status                 string   `gorm:"type:varchar(50);default:'pending'"`
	WinnerID               *uint    `gorm:"default:null"`
	Board                  string   `gorm:"size:255"`
	PlayerTurn             int      `gorm:"check:player_turn IN (1,2)"`
	Player1PointsDelta     float64  `gorm:"default:0"`
	Player2PointsDelta     float64  `gorm:"default:0"`
	Player1LastMoveAt      int64
	Player2LastMoveAt      int64
	Player1RemainingTime   int64   `gorm:"not null"`
	Player2RemainingTime   int64   `gorm:"not null"`
	Player1Rating          float64 `gorm:"not null"`
	Player2Rating          float64 `gorm:"not null"`
	Player1RatingDeviation float64 `gorm:"not null"`
	Player2RatingDeviation float64 `gorm:"not null"`
	DrawOfferedByID        *uint   `gorm:"default:null"`
	DrawOfferedBy          User    `gorm:"foreignKey:DrawOfferedByID"`
	DrawAcceptedByID       *uint   `gorm:"default:null"`
	DrawAcceptedBy         User    `gorm:"foreignKey:DrawAcceptedByID"`
	RematchOfferedByID     *uint   `gorm:"default:null"`
	RematchOfferedBy       User    `gorm:"foreignKey:RematchOfferedByID"`
	RematchAcceptedByID    *uint   `gorm:"default:null"`
	RematchAcceptedBy      User    `gorm:"foreignKey:RematchAcceptedByID"`
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

func (g *Game) BeforeCreate(tx *gorm.DB) error {
	now := time.Now().UnixMilli()

	g.Player1LastMoveAt = now
	g.Player2LastMoveAt = now

	return nil
}

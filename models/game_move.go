package models

import (
	"time"
)

type GameMove struct {
	ID        uint   `gorm:"primaryKey;autoIncrement"`
	GameID    uint   `gorm:"not null"`
	Game      Game   `gorm:"foreignKey:GameID"`
	From      string `gorm:"type:varchar(50);default:'pending'"`
	To        string `gorm:"type:varchar(50);default:'pending'"`
	Board     string `gorm:"size:255"`
	MoveTime  string `gorm:"type:varchar(50)"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

package models

import (
	"time"
)

type UserGameRating struct {
	ID                  uint     `gorm:"primaryKey"`
	UserID              uint     `gorm:"index;not null"`
	GameTypeID          uint     `gorm:"index;not null"`
	Rating              float64  `gorm:"default:1200"`
	Deviation           float64  `gorm:"not null;default:350"`
	User                User     `gorm:"foreignKey:UserID"`
	GameType            GameType `gorm:"foreignKey:GameTypeID"`

	RatingLastUpdatedAt time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

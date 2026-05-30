package models

import (
	"time"
)

type User struct {
	ID        uint             `gorm:"primaryKey;autoIncrement"`
	UserName  string           `gorm:"column:username;unique;not null"`
	Password  string           `gorm:"not null"`
	Email     string           `gorm:"unique;not null"`
	Setting   *Setting         `gorm:"foreignKey:UserID"`
	Ratings   []UserGameRating `gorm:"foreignKey:UserID"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

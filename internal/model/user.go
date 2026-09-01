package model

import "gorm.io/gorm"

type User struct {
	gorm.Model
	UserID   uint64  `gorm:"not null;uniqueIndex"`
	Username string  `gorm:"size:64;not null;uniqueIndex"`
	Password string  `gorm:"size:255;not null"`
	Email    *string `gorm:"size:255"`
	Gender   uint8   `gorm:"not null;default:0"`
}

package model

import "time"

type User struct {
	ID           uint    `gorm:"primarykey"`
	UserID       string  `gorm:"type:char(36);not null;uniqueIndex"`
	Username     string  `gorm:"size:64;not null;uniqueIndex"`
	PasswordHash string  `gorm:"size:255;not null"`
	Email        *string `gorm:"size:255"`
	Gender       uint8   `gorm:"not null;default:0"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

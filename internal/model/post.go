package model

import "time"

type Post struct {
	ID          uint   `gorm:"primaryKey"`
	Title       string `gorm:"size:128;not null"`
	Content     string `gorm:"type:text;not null"`
	AuthorID    string `gorm:"type:char(36);not null;index"`
	CommunityID uint   `gorm:"not null;index"`
	Status      uint8  `gorm:"not null;default:1"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

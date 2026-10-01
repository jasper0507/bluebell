package model

import "gorm.io/gorm"

type Post struct {
	gorm.Model
	Title       string `gorm:"size:128;not null"`
	Content     string `gorm:"type:text;not null"`
	AuthorID    string `gorm:"type:char(36);not null;index"`
	CommunityID uint   `gorm:"not null;index"`
}

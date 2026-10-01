package model

import "gorm.io/gorm"

type Comment struct {
	gorm.Model
	PostID           uint   `gorm:"not null;index"`
	AuthorID         string `gorm:"type:char(36);not null"`
	ReplyToCommentID *uint
	Content          string `gorm:"type:text;not null"`
}

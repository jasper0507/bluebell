package model

import (
	"time"

	"gorm.io/gorm"
)

type Comment struct {
	ID               uint           `gorm:"primaryKey"`
	PostID           uint           `gorm:"not null;index:idx_comments_post_list,priority:1"`
	DeletedAt        gorm.DeletedAt `gorm:"index:idx_comments_post_list,priority:2"`
	CreatedAt        time.Time      `gorm:"index:idx_comments_post_list,priority:3"`
	UpdatedAt        time.Time
	AuthorID         string `gorm:"type:char(36);not null"`
	ReplyToCommentID *uint
	Content          string `gorm:"type:text;not null"`
}

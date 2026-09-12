package model

import "time"

type Community struct {
	ID            uint   `gorm:"primaryKey"`
	CommunityID   uint64 `gorm:"not null;uniqueIndex"`
	CommunityName string `gorm:"size:128;not null;uniqueIndex"`
	Introduction  string `gorm:"size:256;not null"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

package model

type PostVote struct {
	PostID uint   `gorm:"type:bigint unsigned;primaryKey;autoIncrement:false"`
	UserID string `gorm:"type:char(36);primaryKey"`

	Direction int8 `gorm:"type:tinyint;not null;check:chk_post_votes_direction,direction IN (-1,0,1)"`
}

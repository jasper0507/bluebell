package model

// All 返回需要迁移的全部模型
func All() []any {
	return []any{
		&User{},
		&Community{},
		&Post{},
		&Comment{},
		&PostVote{},
		&OutboxEvent{},
	}
}

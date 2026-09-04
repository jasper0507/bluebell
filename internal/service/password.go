package service

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// hashPassword 对密码进行哈希
func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword(
		[]byte(password),
		bcrypt.DefaultCost,
	)

	// 密码哈希失败
	if err != nil {
		return "", fmt.Errorf("密码哈希失败: %w", err)
	}

	return string(hash), nil
}

// verifyPassword 验证密码
func verifyPassword(passwordHash, password string) error {
	return bcrypt.CompareHashAndPassword(
		[]byte(passwordHash),
		[]byte(password),
	)
}

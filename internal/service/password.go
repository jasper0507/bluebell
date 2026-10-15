package service

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// dummyPasswordHash 是随机明文的 DefaultCost 哈希，用户不存在时用它比对，
// 使响应时间与用户存在时一致，避免据此枚举用户名。成本须与 hashPassword 保持一致。
const dummyPasswordHash = "$2a$10$1mzNVn.3xUvwfoTpt/buI.f2YlS1Lb.vffEl2fRcuIj82.LAlX4Ra"

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

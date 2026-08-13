package auth

import "golang.org/x/crypto/bcrypt"

// hashPassword 用 bcrypt 哈希明文密码。
func hashPassword(password string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
}

// checkPassword 校验明文密码与 bcrypt 哈希是否匹配。
func checkPassword(hash []byte, password string) bool {
	return bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil
}

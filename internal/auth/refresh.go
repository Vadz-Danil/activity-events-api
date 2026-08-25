package auth

func NewRefreshToken() (string, error) { return randomToken() }

func HashRefreshToken(token string) []byte { return hashToken(token) }

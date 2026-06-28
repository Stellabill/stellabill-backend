package main

import (
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func main() {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "dev-secret"
	}

	role := envOr("LOADTEST_ROLE", "merchant")
	tenant := envOr("LOADTEST_TENANT", "loadtest-tenant")
	subject := envOr("LOADTEST_SUBJECT", "loadtest-user")

	now := time.Now()
	claims := jwt.MapClaims{
		"sub":    subject,
		"role":   role,
		"roles":  []string{role},
		"tenant": tenant,
		"iat":    now.Unix(),
		"exp":    now.Add(2 * time.Hour).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		fmt.Fprintf(os.Stderr, "sign token: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(signed)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

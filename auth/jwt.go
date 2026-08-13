package auth

import (
	"errors"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func GerarJwt(id, tenantId int64, perfil string) (string, error) {

	secrets := os.Getenv("JWT_SECRET")

	if secrets == "" {

		return "", errors.New("secret nao configurada")
	}

	claim := jwt.MapClaims{

		"sub":      id,
		"tenantId": tenantId,
		"perfil":   perfil,
		"exp":      time.Now().Add(time.Hour * 24).Unix(),
		"iat":      time.Now().Unix(),
	}

	token:= jwt.NewWithClaims(jwt.SigningMethodHS256, claim)

	return token.SignedString([]byte(secrets))
}

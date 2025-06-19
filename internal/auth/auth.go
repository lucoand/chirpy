package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

func HashPassword(password string) (string, error) {
	pwd := []byte(password)
	hash, err := bcrypt.GenerateFromPassword(pwd, 10)
	if err != nil {
		return "", err
	}
	hashedPwd := string(hash)
	return hashedPwd, nil
}

func CheckPasswordHash(hash, password string) error {
	pwd := []byte(password)
	h := []byte(hash)
	return bcrypt.CompareHashAndPassword(h, pwd)
}

func MakeJWT(userID uuid.UUID, tokenSecret string, expiresIn time.Duration) (string, error) {
	if expiresIn <= 0 {
		return "", fmt.Errorf("Error: Negative or zero expiration for token not allowed.")
	}
	method := jwt.SigningMethodHS256
	currentTime := time.Now()
	issuedAt := jwt.NewNumericDate(currentTime)
	expiresAt := jwt.NewNumericDate(currentTime.Add(expiresIn))
	claims := jwt.RegisteredClaims{
		Issuer:    "chirpy",
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
		Subject:   userID.String(),
		ID:        uuid.NewString(),
	}
	token := jwt.NewWithClaims(method, claims)
	signedToken, err := token.SignedString([]byte(tokenSecret))
	if err != nil {
		return "", err
	}
	return signedToken, nil
}

func ValidateJWT(tokenString, tokenSecret string) (uuid.UUID, error) {
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte(tokenSecret), nil
	})

	if err != nil {
		return uuid.UUID{}, err
	}
	if !token.Valid {
		return uuid.UUID{}, fmt.Errorf("Error: Invalid token.")
	}
	stringID, err := token.Claims.GetSubject()
	if err != nil {
		return uuid.UUID{}, err
	}
	id, err := uuid.Parse(stringID)
	if err != nil {
		return uuid.UUID{}, err
	}
	return id, nil
}

func GetBearerToken(headers http.Header) (string, error) {
	return parseAuthorizationHeader(headers, "Bearer")
}

func MakeRefreshToken() (string, error) {
	b := make([]byte, 32)
	n, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	length := len(b)
	if n != length {
		return "", fmt.Errorf("rand.Read wrote %d bytes instead of %d", n, length)
	}
	return hex.EncodeToString(b), nil
}

func parseAuthorizationHeader(headers http.Header, key string) (string, error) {
	authHeader := headers.Get("Authorization")
	if authHeader == "" {
		return "", fmt.Errorf("Empty authorization header.")
	}
	authParts := strings.Split(authHeader, " ")
	if len(authParts) != 2 || authParts[0] != key {
		return "", fmt.Errorf("Bad authorization header.")
	}
	return authParts[1], nil
}

func GetAPIKey(headers http.Header) (string, error) {
	return parseAuthorizationHeader(headers, "ApiKey")
}

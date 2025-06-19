package auth

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAuthGood(t *testing.T) {
	password := "123abc"
	hash, _ := HashPassword(password)
	err := CheckPasswordHash(hash, password)
	if err != nil {
		t.Errorf("Password %s doesn't match hash %s", password, hash)
	}
}

func TestAuthBad(t *testing.T) {
	password := "wxyz789"
	badPass := "123abc"
	hash, _ := HashPassword(password)
	err := CheckPasswordHash(hash, badPass)
	if err == nil {
		t.Errorf("Password %s should not match hash %s", badPass, hash)
	}
}

func TestTokenGood(t *testing.T) {
	userID := uuid.New()
	tokenSecret := "abcdefg"
	expiresIn, err := time.ParseDuration("15m")
	if err != nil {
		t.Errorf("TestTokenGood: Could not parse duration '15m' for some reason: %s", err)
	}
	tokenString, err := MakeJWT(userID, tokenSecret, expiresIn)
	if err != nil {
		t.Errorf("Could not generate token string: %s", err)
	}
	validateID, err := ValidateJWT(tokenString, tokenSecret)
	if err != nil {
		t.Errorf("Could not validate token even though it should have been good: %s", err)
	}
	if userID != validateID {
		t.Errorf("Could not validate ID.  Expected %v but got %v", userID, validateID)
	}
}

func TestTokenBad(t *testing.T) {
	userID := uuid.New()
	goodSecret := "good"
	badSecret := "bad"
	expiresIn, err := time.ParseDuration("15m")
	if err != nil {
		t.Errorf("TestTokenBad: Could not parse duration '15m' for some reason: %s", err)
	}
	tokenString, err := MakeJWT(userID, goodSecret, expiresIn)
	if err != nil {
		t.Errorf("Could not generate token string: %s", err)
	}
	_, err = ValidateJWT(tokenString, badSecret)
	if err == nil {
		t.Errorf("TestTokenBad: Validated token with a bad secret somehow")
	}
}

func TestExpiredToken(t *testing.T) {
	userID := uuid.New()
	tokenSecret := "secret"
	expiresIn, err := time.ParseDuration("1s")
	if err != nil {
		t.Errorf("TestExpiredToken: Could not parse duration '1s' for some reason: %s", err)
	}
	tokenString, err := MakeJWT(userID, tokenSecret, expiresIn)
	if err != nil {
		t.Errorf("Could not generate token string: %s", err)
	}
	time.Sleep(2 * time.Second)
	_, err = ValidateJWT(tokenString, tokenSecret)
	if err == nil {
		t.Errorf("TestExpiredToken: Token should have expired after 1 second but validated anyway even though processing slept for 2 seconds.")
	}
}

func TestGetBearerTokenGood(t *testing.T) {
	headers := http.Header{}
	token := "token1234"
	authHeaderValue := fmt.Sprintf("Bearer %s", token)
	headers.Set("Authorization", authHeaderValue)
	retrievedToken, err := GetBearerToken(headers)
	if err != nil {
		t.Errorf("TestGetBearerTokenGood: couldn't get the token from the header: %s", err)
	}
	if token != retrievedToken {
		t.Errorf("TestGetBearerTokenGood: Token mismatch: expected %s but got %s", token, retrievedToken)
	}
}

func TestGetBearerTokenBad(t *testing.T) {
	headers := http.Header{}
	_, err := GetBearerToken(headers)
	if err == nil {
		t.Errorf("TestGetBearerTokenBad: Somehow worked with empty header.")
	}
	headers.Set("Authorization", "1234")
	_, err = GetBearerToken(headers)
	if err == nil {
		t.Errorf("TestGetBearerTokenBad: Worked with a bad header '1234' (no Bearer).")
	}
	headers.Set("Authorization", "Bear 1234")
	if err == nil {
		t.Errorf("TestGetBearerTokenBad: Worked with a bad header 'Bear 1234'.")
	}
}

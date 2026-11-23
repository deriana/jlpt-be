package usecase

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"jlptmaster/backend/internal/domain"
	"jlptmaster/backend/internal/repository/sqlite"

	"golang.org/x/crypto/bcrypt"
)

var jwtSecret = []byte("jlptmaster-offline-first-secret-key-2026")

type AuthUsecase struct {
	UserRepo *sqlite.UserRepository
}

func NewAuthUsecase(userRepo *sqlite.UserRepository) *AuthUsecase {
	return &AuthUsecase{UserRepo: userRepo}
}

func (u *AuthUsecase) GenerateToken(userID int64, username string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payloadMap := map[string]interface{}{
		"sub":      userID,
		"username": username,
		"exp":      time.Now().Add(30 * 24 * time.Hour).Unix(),
	}
	payloadBytes, _ := json.Marshal(payloadMap)
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	unsigned := header + "." + payload
	mac := hmac.New(sha256.New, jwtSecret)
	mac.Write([]byte(unsigned))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return unsigned + "." + sig
}

func (u *AuthUsecase) ValidateToken(token string) (int64, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, errors.New("invalid token format")
	}
	unsigned := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, jwtSecret)
	mac.Write([]byte(unsigned))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expectedSig), []byte(parts[2])) {
		return 0, errors.New("invalid token signature")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, err
	}
	var data map[string]interface{}
	if err := json.Unmarshal(payloadBytes, &data); err != nil {
		return 0, err
	}
	if sub, ok := data["sub"].(float64); ok {
		return int64(sub), nil
	}
	return 0, errors.New("missing subject in token")
}

func (u *AuthUsecase) Login(identifier string, password string) (*domain.AuthResponse, error) {
	user, err := u.UserRepo.GetByUsernameOrEmail(identifier)
	if err != nil {
		return nil, errors.New("invalid username or password")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, errors.New("invalid username or password")
	}
	token := u.GenerateToken(user.ID, user.Username)
	return &domain.AuthResponse{Token: token, User: *user}, nil
}

func (u *AuthUsecase) Register(username, email, password, targetLevel string) (*domain.AuthResponse, error) {
	if username == "" || email == "" || len(password) < 4 {
		return nil, errors.New("username, email, and password (min 4 chars) are required")
	}
	if targetLevel == "" {
		targetLevel = "N3"
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	user := &domain.User{
		Username:        username,
		Email:           email,
		PasswordHash:    string(hash),
		JLPTTargetLevel: strings.ToUpper(targetLevel),
		DailyGoalMins:   30,
		PreferredVoice:  "3",
		OfflineMode:     true,
		StreakDays:      1,
	}
	if err := u.UserRepo.Create(user); err != nil {
		return nil, fmt.Errorf("could not create user: %w", err)
	}
	token := u.GenerateToken(user.ID, user.Username)
	return &domain.AuthResponse{Token: token, User: *user}, nil
}

func (u *AuthUsecase) GetOrDefaultUser(userID int64) (*domain.User, error) {
	if userID > 0 {
		if usr, err := u.UserRepo.GetByID(userID); err == nil {
			return usr, nil
		}
	}
	return u.UserRepo.GetDefaultUser()
}

func (u *AuthUsecase) UpdateProfile(userID int64, username, email, newPassword, targetLevel string, dailyGoal int, preferredVoice string, offlineMode bool) (*domain.User, error) {
	usr, err := u.GetOrDefaultUser(userID)
	if err != nil {
		return nil, err
	}
	if username != "" {
		usr.Username = username
	}
	if email != "" {
		usr.Email = email
	}
	if targetLevel != "" {
		usr.JLPTTargetLevel = strings.ToUpper(targetLevel)
	}
	if dailyGoal > 0 {
		usr.DailyGoalMins = dailyGoal
	}
	if preferredVoice != "" {
		usr.PreferredVoice = preferredVoice
	}
	usr.OfflineMode = offlineMode

	if newPassword != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		usr.PasswordHash = string(hash)
	} else {
		usr.PasswordHash = ""
	}

	if err := u.UserRepo.UpdateProfile(usr); err != nil {
		return nil, err
	}
	return u.UserRepo.GetByID(usr.ID)
}

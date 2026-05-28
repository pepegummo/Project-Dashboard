package auth

import (
	"context"
	"iot-dashboard/internal/config"
	"iot-dashboard/internal/middleware"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type LoginResponse struct {
	Token string      `json:"token"`
	User  PublicUser  `json:"user"`
}

type PublicUser struct {
	ID             string `json:"id"`
	Email          string `json:"email"`
	Name           string `json:"name"`
	Role           string `json:"role"`
	OrganizationID string `json:"organizationId"`
}

type Service struct {
	repo *Repository
}

func NewService() *Service {
	return &Service{repo: &Repository{}}
}

func (s *Service) Login(ctx context.Context, email, password, ip, userAgent string) (*LoginResponse, error) {
	user, err := s.repo.FindUserByEmail(ctx, email)
	if err != nil || !user.IsActive {
		return nil, middleware.NewAppError(401, "INVALID_CREDENTIALS", "Invalid email or password")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, middleware.NewAppError(401, "INVALID_CREDENTIALS", "Invalid email or password")
	}

	// Build JWT
	expiresIn := 24 * time.Hour
	claims := middleware.JwtClaims{
		Sub:   user.ID,
		OrgId: user.OrganizationID,
		Role:  user.Role,
		Email: user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiresIn)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(config.Env.JwtSecret))
	if err != nil {
		return nil, middleware.NewAppError(500, "TOKEN_ERROR", "Failed to generate token")
	}

	// Fire-and-forget side effects
	_ = s.repo.UpdateLastLogin(ctx, user.ID)
	_ = s.repo.CreateAuditLog(ctx, user.ID, "LOGIN", "user", user.ID, ip, userAgent)

	return &LoginResponse{
		Token: tokenStr,
		User: PublicUser{
			ID:             user.ID,
			Email:          user.Email,
			Name:           user.Name,
			Role:           user.Role,
			OrganizationID: user.OrganizationID,
		},
	}, nil
}

func (s *Service) GetProfile(ctx context.Context, userID string) (*PublicUser, error) {
	user, err := s.repo.FindUserByID(ctx, userID)
	if err != nil {
		return nil, middleware.NewAppError(404, "NOT_FOUND", "User not found")
	}
	return &PublicUser{
		ID:             user.ID,
		Email:          user.Email,
		Name:           user.Name,
		Role:           user.Role,
		OrganizationID: user.OrganizationID,
	}, nil
}

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
	Token         string      `json:"token"`
	User          PublicUser  `json:"user"`
	Organizations []OrgOption `json:"organizations"`
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

	// Token is scoped to the user's home org; the picker can switch it after login.
	tokenStr, err := signToken(user.ID, user.OrganizationID, user.Role, user.Email)
	if err != nil {
		return nil, err
	}

	orgs, err := s.repo.ListOrgs(ctx, user.ID, user.Role)
	if err != nil {
		return nil, middleware.NewAppError(500, "DB_ERROR", "Failed to load organizations")
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
		Organizations: orgs,
	}, nil
}

// SwitchOrg re-issues a JWT scoped to orgID, if the user is allowed to enter it.
func (s *Service) SwitchOrg(ctx context.Context, userID, role, email, orgID string) (string, error) {
	ok, err := s.repo.HasOrgAccess(ctx, userID, role, orgID)
	if err != nil {
		return "", middleware.NewAppError(500, "DB_ERROR", "Failed to check access")
	}
	if !ok {
		return "", middleware.NewAppError(403, "FORBIDDEN", "No access to this organization")
	}
	return signToken(userID, orgID, role, email)
}

// signToken mints a 24h HS256 JWT for the given identity + active org.
func signToken(userID, orgID, role, email string) (string, error) {
	claims := middleware.JwtClaims{
		Sub:   userID,
		OrgId: orgID,
		Role:  role,
		Email: email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	tokenStr, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(config.Env.JwtSecret))
	if err != nil {
		return "", middleware.NewAppError(500, "TOKEN_ERROR", "Failed to generate token")
	}
	return tokenStr, nil
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

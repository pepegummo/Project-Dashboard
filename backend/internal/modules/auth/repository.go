package auth

import (
	"context"
	"iot-dashboard/internal/database"
	"time"
)

type User struct {
	ID             string
	Email          string
	Name           string
	PasswordHash   string
	Role           string
	OrganizationID string
	IsActive       bool
	LastLoginAt    *time.Time
}

type Repository struct{}

func (r *Repository) FindUserByEmail(ctx context.Context, email string) (*User, error) {
	row := database.Pool.QueryRow(ctx, `
		SELECT id, email, name, password_hash, role, organization_id, is_active
		FROM users WHERE email = $1
	`, email)

	u := &User{}
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.OrganizationID, &u.IsActive)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (r *Repository) FindUserByID(ctx context.Context, id string) (*User, error) {
	row := database.Pool.QueryRow(ctx, `
		SELECT id, email, name, role, organization_id, is_active, last_login_at
		FROM users WHERE id = $1
	`, id)

	u := &User{}
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.OrganizationID, &u.IsActive, &u.LastLoginAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

type OrgOption struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	IsMember bool   `json:"isMember"`
}

// ListOrgs returns every organization with an isMember flag for this user.
// Admins are members of all orgs (bypass); others are limited to user_organizations.
func (r *Repository) ListOrgs(ctx context.Context, userID, role string) ([]OrgOption, error) {
	rows, err := database.Pool.Query(ctx, `
		SELECT o.id, o.name,
		       ($2 = 'admin' OR EXISTS (
		           SELECT 1 FROM user_organizations uo
		           WHERE uo.user_id = $1 AND uo.organization_id = o.id
		       )) AS is_member
		FROM organizations o
		ORDER BY o.name ASC
	`, userID, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orgs []OrgOption
	for rows.Next() {
		var o OrgOption
		if err := rows.Scan(&o.ID, &o.Name, &o.IsMember); err != nil {
			return nil, err
		}
		orgs = append(orgs, o)
	}
	return orgs, rows.Err()
}

// HasOrgAccess reports whether the user may enter the given org.
func (r *Repository) HasOrgAccess(ctx context.Context, userID, role, orgID string) (bool, error) {
	if role == "admin" {
		return true, nil
	}
	var exists bool
	err := database.Pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM user_organizations WHERE user_id = $1 AND organization_id = $2)
	`, userID, orgID).Scan(&exists)
	return exists, err
}

func (r *Repository) UpdateLastLogin(ctx context.Context, userID string) error {
	_, err := database.Pool.Exec(ctx, `
		UPDATE users SET last_login_at = NOW() WHERE id = $1
	`, userID)
	return err
}

func (r *Repository) CreateAuditLog(ctx context.Context, userID, action, resource, resourceID, ipAddress, userAgent string) error {
	_, err := database.Pool.Exec(ctx, `
		INSERT INTO audit_logs (id, user_id, action, resource, resource_id, ip_address, user_agent, created_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, NOW())
	`, userID, action, resource, resourceID, ipAddress, userAgent)
	return err
}

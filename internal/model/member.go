package model

import (
	"context"
	"database/sql"
	"time"
)

type TeamMember struct {
	ID             int64          `db:"id"`
	Name           string         `db:"name"`
	GitHubUsername sql.NullString `db:"github_username"`
	LarkUserID     sql.NullString `db:"lark_user_id"`
	LarkOpenID     sql.NullString `db:"lark_open_id"`
	Email          sql.NullString `db:"email"`
	Role           sql.NullString `db:"role"`
	Department     sql.NullString `db:"department"`
	Status         int            `db:"status"`
	CreatedAt      time.Time      `db:"created_at"`
	UpdatedAt      time.Time      `db:"updated_at"`
}

type TeamMemberModel struct {
	db *sql.DB
}

func NewTeamMemberModel(db *sql.DB) *TeamMemberModel {
	return &TeamMemberModel{db: db}
}

func (m *TeamMemberModel) FindByGitHubUsername(ctx context.Context, username string) (*TeamMember, error) {
	query := `SELECT id, name, github_username, lark_user_id, lark_open_id, email, role, department, status, created_at, updated_at
              FROM team_members WHERE github_username = ? AND status = 1`
	row := m.db.QueryRowContext(ctx, query, username)

	var member TeamMember
	err := row.Scan(&member.ID, &member.Name, &member.GitHubUsername, &member.LarkUserID, &member.LarkOpenID,
		&member.Email, &member.Role, &member.Department, &member.Status, &member.CreatedAt, &member.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &member, nil
}

func (m *TeamMemberModel) FindByLarkUserID(ctx context.Context, userID string) (*TeamMember, error) {
	query := `SELECT id, name, github_username, lark_user_id, lark_open_id, email, role, department, status, created_at, updated_at
              FROM team_members WHERE lark_user_id = ? AND status = 1`
	row := m.db.QueryRowContext(ctx, query, userID)

	var member TeamMember
	err := row.Scan(&member.ID, &member.Name, &member.GitHubUsername, &member.LarkUserID, &member.LarkOpenID,
		&member.Email, &member.Role, &member.Department, &member.Status, &member.CreatedAt, &member.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &member, nil
}

func (m *TeamMemberModel) FindByName(ctx context.Context, name string) ([]*TeamMember, error) {
	query := `SELECT id, name, github_username, lark_user_id, lark_open_id, email, role, department, status, created_at, updated_at
              FROM team_members WHERE name LIKE ? AND status = 1`
	rows, err := m.db.QueryContext(ctx, query, "%"+name+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []*TeamMember
	for rows.Next() {
		var member TeamMember
		err := rows.Scan(&member.ID, &member.Name, &member.GitHubUsername, &member.LarkUserID, &member.LarkOpenID,
			&member.Email, &member.Role, &member.Department, &member.Status, &member.CreatedAt, &member.UpdatedAt)
		if err != nil {
			return nil, err
		}
		members = append(members, &member)
	}
	return members, nil
}

func (m *TeamMemberModel) ListAll(ctx context.Context) ([]*TeamMember, error) {
	query := `SELECT id, name, github_username, lark_user_id, lark_open_id, email, role, department, status, created_at, updated_at
              FROM team_members WHERE status = 1 ORDER BY name`
	rows, err := m.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []*TeamMember
	for rows.Next() {
		var member TeamMember
		err := rows.Scan(&member.ID, &member.Name, &member.GitHubUsername, &member.LarkUserID, &member.LarkOpenID,
			&member.Email, &member.Role, &member.Department, &member.Status, &member.CreatedAt, &member.UpdatedAt)
		if err != nil {
			return nil, err
		}
		members = append(members, &member)
	}
	return members, nil
}

func (m *TeamMemberModel) Upsert(ctx context.Context, member *TeamMember) error {
	query := `INSERT INTO team_members (name, github_username, lark_user_id, lark_open_id, email, role, department)
              VALUES (?, ?, ?, ?, ?, ?, ?)
              ON DUPLICATE KEY UPDATE name = VALUES(name), lark_user_id = VALUES(lark_user_id),
              lark_open_id = VALUES(lark_open_id), email = VALUES(email), role = VALUES(role), department = VALUES(department)`
	_, err := m.db.ExecContext(ctx, query, member.Name, member.GitHubUsername, member.LarkUserID,
		member.LarkOpenID, member.Email, member.Role, member.Department)
	return err
}

package model

import (
	"context"
	"database/sql"
	"time"
)

type GitCommit struct {
	ID            int64          `db:"id"`
	MemberID      sql.NullInt64  `db:"member_id"`
	AuthorName    string         `db:"author_name"`
	AuthorEmail   sql.NullString `db:"author_email"`
	RepoName      string         `db:"repo_name"`
	RepoFullName  sql.NullString `db:"repo_full_name"`
	Branch        sql.NullString `db:"branch"`
	CommitSHA     string         `db:"commit_sha"`
	CommitMessage sql.NullString `db:"commit_message"`
	FilesChanged  int            `db:"files_changed"`
	Additions     int            `db:"additions"`
	Deletions     int            `db:"deletions"`
	CommittedAt   time.Time      `db:"committed_at"`
	CreatedAt     time.Time      `db:"created_at"`
}

type CommitStats struct {
	AuthorName   string `db:"author_name"`
	MemberID     int64  `db:"member_id"`
	CommitCount  int    `db:"commit_count"`
	FilesChanged int    `db:"files_changed"`
	Additions    int    `db:"additions"`
	Deletions    int    `db:"deletions"`
	RepoCount    int    `db:"repo_count"`
}

type GitCommitModel struct {
	db *sql.DB
}

func NewGitCommitModel(db *sql.DB) *GitCommitModel {
	return &GitCommitModel{db: db}
}

func (m *GitCommitModel) Insert(ctx context.Context, commit *GitCommit) error {
	query := `INSERT INTO git_commits (member_id, author_name, author_email, repo_name, repo_full_name, branch,
              commit_sha, commit_message, files_changed, additions, deletions, committed_at)
              VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
              ON DUPLICATE KEY UPDATE files_changed = VALUES(files_changed), additions = VALUES(additions), deletions = VALUES(deletions)`
	_, err := m.db.ExecContext(ctx, query, commit.MemberID, commit.AuthorName, commit.AuthorEmail,
		commit.RepoName, commit.RepoFullName, commit.Branch, commit.CommitSHA, commit.CommitMessage,
		commit.FilesChanged, commit.Additions, commit.Deletions, commit.CommittedAt)
	return err
}

func (m *GitCommitModel) BatchInsert(ctx context.Context, commits []*GitCommit) error {
	for _, commit := range commits {
		if err := m.Insert(ctx, commit); err != nil {
			return err
		}
	}
	return nil
}

// GetStatsByMember 获取成员在指定时间范围内的提交统计
func (m *GitCommitModel) GetStatsByMember(ctx context.Context, memberID int64, start, end time.Time) (*CommitStats, error) {
	query := `SELECT author_name, COALESCE(member_id, 0) as member_id,
              COUNT(*) as commit_count, SUM(files_changed) as files_changed,
              SUM(additions) as additions, SUM(deletions) as deletions,
              COUNT(DISTINCT repo_name) as repo_count
              FROM git_commits
              WHERE member_id = ? AND committed_at BETWEEN ? AND ?
              GROUP BY member_id, author_name`
	row := m.db.QueryRowContext(ctx, query, memberID, start, end)

	var stats CommitStats
	err := row.Scan(&stats.AuthorName, &stats.MemberID, &stats.CommitCount, &stats.FilesChanged,
		&stats.Additions, &stats.Deletions, &stats.RepoCount)
	if err != nil {
		return nil, err
	}
	return &stats, nil
}

// GetStatsByAuthorName 按作者名称查询统计
func (m *GitCommitModel) GetStatsByAuthorName(ctx context.Context, authorName string, start, end time.Time) (*CommitStats, error) {
	query := `SELECT author_name, COALESCE(member_id, 0) as member_id,
              COUNT(*) as commit_count, SUM(files_changed) as files_changed,
              SUM(additions) as additions, SUM(deletions) as deletions,
              COUNT(DISTINCT repo_name) as repo_count
              FROM git_commits
              WHERE author_name LIKE ? AND committed_at BETWEEN ? AND ?
              GROUP BY author_name`
	row := m.db.QueryRowContext(ctx, query, "%"+authorName+"%", start, end)

	var stats CommitStats
	err := row.Scan(&stats.AuthorName, &stats.MemberID, &stats.CommitCount, &stats.FilesChanged,
		&stats.Additions, &stats.Deletions, &stats.RepoCount)
	if err != nil {
		return nil, err
	}
	return &stats, nil
}

// GetAllStats 获取所有人在指定时间范围的统计
func (m *GitCommitModel) GetAllStats(ctx context.Context, start, end time.Time) ([]*CommitStats, error) {
	query := `SELECT author_name, COALESCE(member_id, 0) as member_id,
              COUNT(*) as commit_count, SUM(files_changed) as files_changed,
              SUM(additions) as additions, SUM(deletions) as deletions,
              COUNT(DISTINCT repo_name) as repo_count
              FROM git_commits
              WHERE committed_at BETWEEN ? AND ?
              GROUP BY author_name, member_id
              ORDER BY commit_count DESC`
	rows, err := m.db.QueryContext(ctx, query, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var statsList []*CommitStats
	for rows.Next() {
		var stats CommitStats
		err := rows.Scan(&stats.AuthorName, &stats.MemberID, &stats.CommitCount, &stats.FilesChanged,
			&stats.Additions, &stats.Deletions, &stats.RepoCount)
		if err != nil {
			return nil, err
		}
		statsList = append(statsList, &stats)
	}
	return statsList, nil
}

// GetRecentCommits 获取最近的提交
func (m *GitCommitModel) GetRecentCommits(ctx context.Context, memberID int64, limit int) ([]*GitCommit, error) {
	query := `SELECT id, member_id, author_name, author_email, repo_name, repo_full_name, branch,
              commit_sha, commit_message, files_changed, additions, deletions, committed_at, created_at
              FROM git_commits
              WHERE member_id = ?
              ORDER BY committed_at DESC LIMIT ?`
	rows, err := m.db.QueryContext(ctx, query, memberID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var commits []*GitCommit
	for rows.Next() {
		var commit GitCommit
		err := rows.Scan(&commit.ID, &commit.MemberID, &commit.AuthorName, &commit.AuthorEmail,
			&commit.RepoName, &commit.RepoFullName, &commit.Branch, &commit.CommitSHA, &commit.CommitMessage,
			&commit.FilesChanged, &commit.Additions, &commit.Deletions, &commit.CommittedAt, &commit.CreatedAt)
		if err != nil {
			return nil, err
		}
		commits = append(commits, &commit)
	}
	return commits, nil
}

// GetCommitsByDateRange 按日期范围查询提交
func (m *GitCommitModel) GetCommitsByDateRange(ctx context.Context, authorName string, start, end time.Time, limit int) ([]*GitCommit, error) {
	query := `SELECT id, member_id, author_name, author_email, repo_name, repo_full_name, branch,
              commit_sha, commit_message, files_changed, additions, deletions, committed_at, created_at
              FROM git_commits
              WHERE author_name LIKE ? AND committed_at BETWEEN ? AND ?
              ORDER BY committed_at DESC LIMIT ?`
	rows, err := m.db.QueryContext(ctx, query, "%"+authorName+"%", start, end, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var commits []*GitCommit
	for rows.Next() {
		var commit GitCommit
		err := rows.Scan(&commit.ID, &commit.MemberID, &commit.AuthorName, &commit.AuthorEmail,
			&commit.RepoName, &commit.RepoFullName, &commit.Branch, &commit.CommitSHA, &commit.CommitMessage,
			&commit.FilesChanged, &commit.Additions, &commit.Deletions, &commit.CommittedAt, &commit.CreatedAt)
		if err != nil {
			return nil, err
		}
		commits = append(commits, &commit)
	}
	return commits, nil
}

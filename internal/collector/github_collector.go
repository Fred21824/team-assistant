package collector

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"time"

	"team-assistant/internal/model"
	"team-assistant/internal/svc"
	"team-assistant/pkg/github"
)

// GitHubCollector GitHub数据采集器
type GitHubCollector struct {
	svcCtx       *svc.ServiceContext
	githubClient *github.Client
	interval     time.Duration
	stopChan     chan struct{}
}

// NewGitHubCollector 创建GitHub采集器
func NewGitHubCollector(svcCtx *svc.ServiceContext, interval time.Duration) *GitHubCollector {
	return &GitHubCollector{
		svcCtx:       svcCtx,
		githubClient: github.NewClient(svcCtx.Config.GitHub.Token),
		interval:     interval,
		stopChan:     make(chan struct{}),
	}
}

// Start 启动定时采集
func (c *GitHubCollector) Start() {
	log.Printf("GitHub collector started, interval: %v", c.interval)

	// 立即执行一次
	go c.collect()

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			go c.collect()
		case <-c.stopChan:
			log.Println("GitHub collector stopped")
			return
		}
	}
}

// Stop 停止采集
func (c *GitHubCollector) Stop() {
	close(c.stopChan)
}

// collect 执行一次采集
func (c *GitHubCollector) collect() {
	ctx := context.Background()

	log.Println("Starting GitHub data collection...")

	// 获取过去24小时的提交
	since := time.Now().Add(-24 * time.Hour)
	until := time.Now()

	totalCommits := 0

	for _, org := range c.svcCtx.Config.GitHub.Organizations {
		// 获取组织的所有仓库
		repos, err := c.githubClient.ListOrgRepos(ctx, org)
		if err != nil {
			log.Printf("Failed to list repos for org %s: %v", org, err)
			// 尝试作为用户获取
			repos, err = c.githubClient.ListUserRepos(ctx, org)
			if err != nil {
				log.Printf("Failed to list repos for user %s: %v", org, err)
				continue
			}
		}

		log.Printf("Found %d repos for %s", len(repos), org)

		for _, repo := range repos {
			commits, err := c.githubClient.GetCommits(ctx, org, repo.Name, since, until)
			if err != nil {
				log.Printf("Failed to get commits for %s/%s: %v", org, repo.Name, err)
				continue
			}

			if len(commits) == 0 {
				continue
			}

			log.Printf("Found %d commits in %s/%s", len(commits), org, repo.Name)

			for _, commit := range commits {
				gitCommit := &model.GitCommit{
					CommitSHA:     commit.SHA,
					RepoName:      repo.Name,
					RepoFullName:  sql.NullString{String: repo.FullName, Valid: true},
					AuthorName:    commit.Commit.Author.Name,
					AuthorEmail:   sql.NullString{String: commit.Commit.Author.Email, Valid: true},
					CommitMessage: sql.NullString{String: truncateMessage(commit.Commit.Message), Valid: true},
					CommittedAt:   commit.Commit.Author.Date,
					Additions:     commit.Stats.Additions,
					Deletions:     commit.Stats.Deletions,
					FilesChanged:  len(commit.Files),
				}

				// 尝试关联成员
				if commit.Author != nil && commit.Author.Login != "" {
					member, err := c.svcCtx.MemberModel.FindByGitHubUsername(ctx, commit.Author.Login)
					if err == nil {
						gitCommit.MemberID = sql.NullInt64{Int64: member.ID, Valid: true}
					}
				}

				if err := c.svcCtx.CommitModel.Insert(ctx, gitCommit); err != nil {
					log.Printf("Failed to save commit %s: %v", commit.SHA[:7], err)
				} else {
					totalCommits++
				}
			}
		}
	}

	log.Printf("GitHub collection completed, saved %d commits", totalCommits)
}

// CollectOnce 手动触发一次采集
func (c *GitHubCollector) CollectOnce() {
	c.collect()
}

// truncateMessage 截断提交信息
func truncateMessage(msg string) string {
	// 只保留第一行
	if idx := strings.Index(msg, "\n"); idx != -1 {
		msg = msg[:idx]
	}
	// 限制长度
	if len(msg) > 500 {
		msg = msg[:500]
	}
	return msg
}

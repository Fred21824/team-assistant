package svc

import (
	"database/sql"
	"fmt"

	"team-assistant/internal/config"
	"team-assistant/internal/model"
	"team-assistant/pkg/lark"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

type ServiceContext struct {
	Config config.Config

	// Database
	DB *sql.DB

	// Redis
	Redis *redis.Client

	// Lark client
	LarkClient *lark.Client

	// Models
	MemberModel   *model.TeamMemberModel
	CommitModel   *model.GitCommitModel
	MessageModel  *model.ChatMessageModel
	GroupModel    *model.ChatGroupModel
	SyncTaskModel *model.MessageSyncTaskModel
}

func NewServiceContext(c config.Config) (*ServiceContext, error) {
	// Initialize MySQL
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		c.MySQL.User, c.MySQL.Password, c.MySQL.Host, c.MySQL.Database)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MySQL: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping MySQL: %w", err)
	}

	// Initialize Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     c.Redis.Host,
		Password: c.Redis.Password,
		DB:       c.Redis.DB,
	})

	// Initialize Lark client
	larkClient := lark.NewClient(c.Lark.Domain, c.Lark.AppID, c.Lark.AppSecret)

	return &ServiceContext{
		Config:        c,
		DB:            db,
		Redis:         rdb,
		LarkClient:    larkClient,
		MemberModel:   model.NewTeamMemberModel(db),
		CommitModel:   model.NewGitCommitModel(db),
		MessageModel:  model.NewChatMessageModel(db),
		GroupModel:    model.NewChatGroupModel(db),
		SyncTaskModel: model.NewMessageSyncTaskModel(db),
	}, nil
}

func (s *ServiceContext) Close() {
	if s.DB != nil {
		s.DB.Close()
	}
	if s.Redis != nil {
		s.Redis.Close()
	}
}

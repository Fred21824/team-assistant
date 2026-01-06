package main

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"gopkg.in/yaml.v3"

	"team-assistant/internal/config"
	"team-assistant/pkg/embedding"
	"team-assistant/pkg/vectordb"
)

// messageIDToUUID 将消息ID转换为UUID格式
func messageIDToUUID(messageID string) string {
	hash := md5.Sum([]byte(messageID))
	hex := hex.EncodeToString(hash[:])
	// 格式化为 UUID: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	return fmt.Sprintf("%s-%s-%s-%s-%s", hex[0:8], hex[8:12], hex[12:16], hex[16:20], hex[20:32])
}

func main() {
	// 命令行参数
	limit := flag.Int("limit", 0, "Max messages to index (0 = all)")
	workers := flag.Int("workers", 5, "Number of concurrent workers")
	recreate := flag.Bool("recreate", false, "Recreate collection (required when changing embedding model)")
	flag.Parse()

	// 加载配置
	data, err := os.ReadFile("etc/config.yaml")
	if err != nil {
		log.Fatalf("Failed to read config: %v", err)
	}

	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("Failed to parse config: %v", err)
	}

	// 检查 VectorDB 是否启用
	if !cfg.VectorDB.Enabled {
		log.Fatal("VectorDB is not enabled in config")
	}

	// 连接数据库
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true&tls=skip-verify",
		cfg.MySQL.User,
		cfg.MySQL.Password,
		cfg.MySQL.Host,
		cfg.MySQL.Database,
	)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Failed to connect to MySQL: %v", err)
	}
	defer db.Close()

	// 初始化客户端
	dimension := cfg.VectorDB.EmbeddingDimension
	if dimension <= 0 {
		dimension = 768 // 默认维度
	}

	embClient := embedding.NewOllamaClientWithDimension(cfg.VectorDB.OllamaEndpoint, cfg.VectorDB.EmbeddingModel, dimension)
	vectorClient := vectordb.NewQdrantClient(cfg.VectorDB.QdrantEndpoint)

	ctx := context.Background()

	// 重建或创建集合
	if *recreate {
		log.Printf("Recreating collection %s with dimension %d...", cfg.VectorDB.CollectionName, dimension)
		if err := vectorClient.RecreateCollection(ctx, cfg.VectorDB.CollectionName, dimension); err != nil {
			log.Fatalf("Failed to recreate collection: %v", err)
		}
		log.Printf("Collection recreated successfully")
	} else {
		// 确保集合存在
		if err := vectorClient.CreateCollection(ctx, cfg.VectorDB.CollectionName, dimension); err != nil {
			log.Printf("Collection may already exist: %v", err)
		}
	}

	// 查询消息
	query := `
		SELECT m.message_id, m.chat_id, COALESCE(g.chat_name, '') as chat_name,
			   COALESCE(m.sender_id, '') as sender_id, COALESCE(m.sender_name, '') as sender_name,
			   m.content, m.created_at
		FROM chat_messages m
		LEFT JOIN chat_groups g ON m.chat_id = g.chat_id
		WHERE m.content IS NOT NULL AND m.content != ''
		ORDER BY m.created_at DESC
	`
	if *limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", *limit)
	}

	log.Println("Querying messages from database...")
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		log.Fatalf("Failed to query messages: %v", err)
	}
	defer rows.Close()

	type Message struct {
		MessageID  string
		ChatID     string
		ChatName   string
		SenderID   string
		SenderName string
		Content    string
		CreatedAt  time.Time
	}

	var messages []Message
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.MessageID, &msg.ChatID, &msg.ChatName,
			&msg.SenderID, &msg.SenderName, &msg.Content, &msg.CreatedAt); err != nil {
			log.Printf("Failed to scan row: %v", err)
			continue
		}
		messages = append(messages, msg)
	}

	total := len(messages)
	log.Printf("Found %d messages to index (workers: %d)", total, *workers)

	if total == 0 {
		return
	}

	// 并发处理
	var indexed int64
	var failed int64
	var wg sync.WaitGroup
	msgChan := make(chan Message, 100)

	// 启动 worker
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for msg := range msgChan {
				log.Printf("Processing msg %s (len=%d)", msg.MessageID, len(msg.Content))
				vec, err := embClient.GetEmbedding(ctx, msg.Content)
				if err != nil {
					f := atomic.AddInt64(&failed, 1)
					log.Printf("Embedding error [%d]: %v", f, err)
					continue
				}

				log.Printf("Got vector with %d dimensions", len(vec))

				if len(vec) == 0 {
					log.Printf("Empty vector for msg %s", msg.MessageID)
					atomic.AddInt64(&failed, 1)
					continue
				}

				point := vectordb.Point{
					ID:     messageIDToUUID(msg.MessageID),
					Vector: vec,
					Payload: map[string]interface{}{
						"message_id":  msg.MessageID,
						"chat_id":     msg.ChatID,
						"chat_name":   msg.ChatName,
						"sender_id":   msg.SenderID,
						"sender_name": msg.SenderName,
						"content":     msg.Content,
						"created_at":  msg.CreatedAt.Format(time.RFC3339),
					},
				}

				if err := vectorClient.Upsert(ctx, cfg.VectorDB.CollectionName, []vectordb.Point{point}); err != nil {
					log.Printf("Upsert error: %v", err)
					atomic.AddInt64(&failed, 1)
					continue
				}

				n := atomic.AddInt64(&indexed, 1)
				if n%50 == 0 {
					log.Printf("Progress: %d/%d (%.1f%%)", n, total, float64(n)/float64(total)*100)
				}
			}
		}()
	}

	// 发送消息
	start := time.Now()
	for _, msg := range messages {
		msgChan <- msg
	}
	close(msgChan)
	wg.Wait()

	log.Printf("Done! Indexed: %d, Failed: %d, Time: %v", indexed, failed, time.Since(start))
}

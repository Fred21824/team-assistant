package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"gopkg.in/yaml.v3"

	"team-assistant/internal/config"
	"team-assistant/pkg/lark"
)

func main() {
	data, _ := os.ReadFile("etc/config.yaml")
	var cfg config.Config
	yaml.Unmarshal(data, &cfg)

	client := lark.NewClient(cfg.Lark.Domain, cfg.Lark.AppID, cfg.Lark.AppSecret)
	ctx := context.Background()

	testOpenID := "ou_353b0f0a3a4fd36dc3fbd7786240b043"
	chatID := "oc_0dc23a40dbd0b12a0707abaadeaced28" // 虚拟币支付群

	// 获取 token
	token, _ := client.GetTenantAccessToken(ctx)

	// 1. 测试通讯录 API
	fmt.Println("=== 测试通讯录 API ===")
	url := fmt.Sprintf("%s/open-apis/contact/v3/users/%s?user_id_type=open_id", cfg.Lark.Domain, testOpenID)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := http.DefaultClient.Do(req)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Printf("Contact API:\n%s\n\n", string(body))

	// 2. 测试群成员 API
	fmt.Println("=== 测试群成员 API ===")
	url2 := fmt.Sprintf("%s/open-apis/im/v1/chats/%s/members?member_id_type=open_id&page_size=10", cfg.Lark.Domain, chatID)
	req2, _ := http.NewRequest("GET", url2, nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	resp2, _ := http.DefaultClient.Do(req2)
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	fmt.Printf("Chat Members API:\n%s\n", string(body2))
}

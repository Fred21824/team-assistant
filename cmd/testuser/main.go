package main

import (
	"context"
	"fmt"
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

	// 测试获取用户信息
	testOpenID := "ou_eeefe1e372584cd0a133ed761f0c8d39"
	userInfo, err := client.GetUserInfo(ctx, testOpenID)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("User Name: %s\n", userInfo.Name)
	fmt.Printf("User EnName: %s\n", userInfo.EnName)
}

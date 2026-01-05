package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"gopkg.in/yaml.v3"
	"team-assistant/internal/config"
	"team-assistant/pkg/lark"
)

func main() {
	// 加载配置
	data, _ := os.ReadFile("etc/config.yaml")
	var cfg config.Config
	yaml.Unmarshal(data, &cfg)

	client := lark.NewClient(cfg.Lark.Domain, cfg.Lark.AppID, cfg.Lark.AppSecret)
	ctx := context.Background()

	token, err := client.GetTenantAccessToken(ctx)
	if err != nil {
		fmt.Printf("获取 token 失败: %v\n", err)
		return
	}

	fmt.Println("=== 查询应用可访问的文档 ===\n")

	// 1. 查询云盘根目录文件列表
	fmt.Println("【1. 云盘文件列表】")
	listDriveFiles(ctx, cfg.Lark.Domain, token, "")

	// 2. 查询知识库空间列表
	fmt.Println("\n【2. 知识库空间列表】")
	listWikiSpaces(ctx, cfg.Lark.Domain, token)

	// 3. 查询最近访问的文档
	fmt.Println("\n【3. 搜索所有文档】")
	searchDocs(ctx, cfg.Lark.Domain, token)
}

// listDriveFiles 列出云盘文件
func listDriveFiles(ctx context.Context, domain, token, folderToken string) {
	url := fmt.Sprintf("%s/open-apis/drive/v1/files?page_size=50", domain)
	if folderToken != "" {
		url += "&folder_token=" + folderToken
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("请求失败: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Files []struct {
				Token      string `json:"token"`
				Name       string `json:"name"`
				Type       string `json:"type"`
				URL        string `json:"url"`
				CreatedTime int64 `json:"created_time"`
			} `json:"files"`
			HasMore   bool   `json:"has_more"`
			NextPageToken string `json:"next_page_token"`
		} `json:"data"`
	}
	json.Unmarshal(body, &result)

	if result.Code != 0 {
		fmt.Printf("错误: %s (code: %d)\n", result.Msg, result.Code)
		fmt.Printf("原始响应: %s\n", string(body))
		return
	}

	if len(result.Data.Files) == 0 {
		fmt.Println("没有找到文件")
		return
	}

	for i, file := range result.Data.Files {
		fmt.Printf("%d. [%s] %s\n", i+1, file.Type, file.Name)
		fmt.Printf("   Token: %s\n", file.Token)
		if file.URL != "" {
			fmt.Printf("   URL: %s\n", file.URL)
		}
	}
	fmt.Printf("\n共 %d 个文件, 还有更多: %v\n", len(result.Data.Files), result.HasMore)
}

// listWikiSpaces 列出知识库空间
func listWikiSpaces(ctx context.Context, domain, token string) {
	url := fmt.Sprintf("%s/open-apis/wiki/v2/spaces?page_size=50", domain)

	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("请求失败: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Items []struct {
				SpaceID     string `json:"space_id"`
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"items"`
		} `json:"data"`
	}
	json.Unmarshal(body, &result)

	if result.Code != 0 {
		fmt.Printf("错误: %s (code: %d)\n", result.Msg, result.Code)
		fmt.Printf("原始响应: %s\n", string(body))
		return
	}

	if len(result.Data.Items) == 0 {
		fmt.Println("没有找到知识库空间")
		return
	}

	for i, space := range result.Data.Items {
		fmt.Printf("%d. %s (ID: %s)\n", i+1, space.Name, space.SpaceID)
		if space.Description != "" {
			fmt.Printf("   描述: %s\n", space.Description)
		}
	}
}

// searchDocs 搜索文档
func searchDocs(ctx context.Context, domain, token string) {
	// 使用搜索 API 查找所有类型的文档
	url := fmt.Sprintf("%s/open-apis/suite/docs-api/search/object", domain)

	reqBody := map[string]interface{}{
		"search_key": "*",  // 搜索所有
		"count":      50,
		"offset":     0,
		"owner_ids":  []string{},
		"docs_types": []string{"doc", "docx", "sheet", "bitable", "mindnote", "wiki"},
	}
	jsonBody, _ := json.Marshal(reqBody)

	req, _ := http.NewRequestWithContext(ctx, "POST", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	// 尝试另一个 API
	url2 := fmt.Sprintf("%s/open-apis/drive/v1/files?page_size=100&order_by=EditedTime&direction=DESC", domain)
	req2, _ := http.NewRequestWithContext(ctx, "GET", url2, nil)
	req2.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req2)
	if err != nil {
		fmt.Printf("请求失败: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("原始响应:\n%s\n", string(body))

	_ = jsonBody // 避免未使用警告
}

package main

import (
	"context"
	"fmt"
	"team-assistant/pkg/lark"
)

func main() {
	client := lark.NewClient(
		"https://open.larksuite.com",
		"cli_a8810599b478d028",
		"A6mNclZDCu0f9qNZ1FnEkb2rXv3pk2f5",
	)

	ctx := context.Background()
	appToken := "DMGPbsyU4azQpmsUFgcl8UKHgnh"
	tableID := "tblbJLbc7ioORThL"

	// 测试1: 通过站点前缀查询
	fmt.Println("=== 测试1: 通过站点前缀查询 (by4) ===")
	record, err := client.GetSiteInfoByPrefix(ctx, appToken, tableID, "by4")
	if err != nil {
		fmt.Printf("查询失败: %v\n", err)
	} else if record == nil {
		fmt.Println("未找到记录")
	} else {
		for k, v := range record.Fields {
			fmt.Printf("  %s: %v\n", k, v)
		}
	}

	// 测试2: 通过站点ID查询
	fmt.Println("\n=== 测试2: 通过站点ID查询 (3031) ===")
	record2, err := client.GetSiteInfoBySiteID(ctx, appToken, tableID, "3031")
	if err != nil {
		fmt.Printf("查询失败: %v\n", err)
	} else if record2 == nil {
		fmt.Println("未找到记录")
	} else {
		for k, v := range record2.Fields {
			fmt.Printf("  %s: %v\n", k, v)
		}
	}
}

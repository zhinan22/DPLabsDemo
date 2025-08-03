package main

import (
	"github.com/zhinan22/DPLabsDemo/config"
	"github.com/zhinan22/DPLabsDemo/handlers"
	"github.com/zhinan22/DPLabsDemo/services"
	"log"
	_ "os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	// 加载环境变量
	err := godotenv.Load()
	if err != nil {
		log.Printf("警告: 无法加载.env文件, 使用系统环境变量: %v", err)
	}

	// 初始化配置
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 初始化Solana服务
	solanaService, _ := services.NewPnlService(cfg.SolanaRPCUrl, cfg.JupiterProgramID, cfg.OKXClient)

	// 初始化处理器
	handler := handlers.NewPnLHandler(solanaService)

	//	curl "http://localhost:8080/pnl?userAddress=8deJ9xeUvXSJwicYptA9mHsU2rN2pDx37KWzkDkEXhU6&tokenMint=2dMHTBnkSPRNqasqwpPfK4wwPxNdgmb1LhrbJ8vGjupsv&limit=200"
	// 设置Gin路由
	r := gin.Default()
	r.GET("/pnl", handler.GetPnL)

	// 启动服务器
	log.Printf("服务器启动在端口 %s", cfg.ServerPort)
	log.Fatal(r.Run(":" + cfg.ServerPort))
}

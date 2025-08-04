package test

import (
	"github.com/gin-gonic/gin"
	"github.com/go-playground/assert/v2"
	"github.com/joho/godotenv"
	"github.com/zhinan22/DPLabsDemo/config"
	"github.com/zhinan22/DPLabsDemo/handlers"
	"github.com/zhinan22/DPLabsDemo/services"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 测试用的响应结构体（与实际保持一致）
type PnLResponse struct {
	Results []services.PnLResult `json:"results,omitempty"`
}

// 初始化测试环境
func setupTest() (*gin.Engine, *services.PnlService) {
	// 设置gin为测试模式
	gin.SetMode(gin.TestMode)

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

	// 设置路由
	r := gin.Default()
	r.GET("/pnl", handler.GetPnL)

	return r, solanaService
}

func Test_Pnl(t *testing.T) {

	r, _ := setupTest()

	req := httptest.NewRequest("GET", "/pnl", nil)
	q := req.URL.Query()
	q.Add("userAddress", "DxhVG5CzS5GHWkpZKtnGYYAsmUbE7FgdYbMYK6FGQ8hP")
	q.Add("tokenMint", "6p6xgHyF7AeE6TZkSmFsko444wqoP15icUSqi2jfGiPN")
	q.Add("limit", "30") // 非数字的limit
	req.URL.RawQuery = q.Encode()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 验证应使用默认limit（100），但不会返回错误
	assert.Equal(t, http.StatusOK, w.Code)

}

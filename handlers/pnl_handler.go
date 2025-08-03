package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zhinan22/DPLabsDemo/services"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// PnLHandler 处理PnL相关请求
type PnLHandler struct {
	PnlService *services.PnlService
}

// NewPnLHandler 创建新的PnL处理器
func NewPnLHandler(PnlService *services.PnlService) *PnLHandler {
	return &PnLHandler{
		PnlService: PnlService,
	}
}

// PnLResponse API响应结构
type PnLResponse struct {
	ClosedPositions []ClosedPosition `json:"closedPositions,omitempty"`
	OpenPosition    *OpenPosition    `json:"openPosition,omitempty"`
	Error           string           `json:"error,omitempty"`
}

// ClosedPosition 已平仓头寸
type ClosedPosition struct {
	AverageCost          float64 `json:"averageCost"`
	ProfitLossPercentage string  `json:"profitLossPercentage"`
	ProfitLossValue      float64 `json:"profitLossValue"`
}

// OpenPosition 持仓中头寸
type OpenPosition struct {
	AverageCost               float64 `json:"averageCost"`
	ProfitLossPercentage      string  `json:"profitLossPercentage"`
	RealizedProfitLossValue   float64 `json:"realizedProfitLossValue"`
	UnrealizedProfitLossValue float64 `json:"unrealizedProfitLossValue"`
}

// GetPnL 处理PnL查询请求
func (h *PnLHandler) GetPnL(c *gin.Context) {
	// 获取请求参数
	userAddress := c.Query("userAddress")
	tokenMint := c.Query("tokenMint")
	limitStr := c.DefaultQuery("limit", "100")

	// 验证必要参数
	if userAddress == "" || tokenMint == "" {
		c.JSON(http.StatusBadRequest, PnLResponse{
			Error: "缺少必要参数: userAddress和tokenMint都是必需的",
		})
		return
	}

	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		// 转换失败（如字符串不是数字）
		fmt.Printf("转换失败: %v\n", err)
		return
	}

	// 获取用户与Jupiter的交易
	transactions, err := h.PnlService.GetTransactions(
		c.Request.Context(),
		userAddress,
		limit,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, PnLResponse{
			Error: "获取交易记录失败: " + err.Error(),
		})
		return
	}

	results, err := h.PnlService.CalculatePnL(context.Background(), transactions, userAddress, tokenMint)
	if err != nil {
		c.JSON(http.StatusInternalServerError, PnLResponse{
			Error: "获取交易记录失败: " + err.Error(),
		})
	}

	response := results
	// 将数组格式化为带缩进的 JSON
	jsonData, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		fmt.Printf("JSON 格式化失败: %v\n", err)
		return
	}

	// 打印格式化后的 JSON
	fmt.Println("PnL 结果数组:")
	fmt.Println(string(jsonData))
	c.JSON(http.StatusOK, response)
}

// 辅助函数：将字符串转换为整数
func parseInt(s string) (int, error) {
	// 实现字符串到整数的转换逻辑
	// 简化示例，实际应使用strconv.Atoi

	return strconv.Atoi(s)
}

package services

import (
	"context"
	"fmt"
	"github.com/gagliardetto/solana-go/rpc"
	"math"
	"strconv"
)

// Position 跟踪持仓状态（新增AverageCost字段记录历史平均成本）
type Position struct {
	TotalAmount     float64 // 当前持仓数量（平仓后为0）
	TotalCostUSD    float64 // 当前持仓成本（平仓后为0）
	RealizedPnL     float64 // 已实现盈亏
	TotalInvestment float64 // 该持仓的总投入成本（历史累计，平仓后不变）
	TotalQuantity   float64 // 该持仓的总数量（历史累计，平仓后不变）
	AverageCost     float64 // 平均成本（历史值，平仓后保留）
	Transactions    []Order // 相关交易记录
	IsClosed        bool    // 是否已平仓
}

// calculatePnL 计算PnL（修正平均成本和总投资记录逻辑）
func (s *PnlService) calculatePnL(ctx context.Context, orders []Order, targetMint string) ([]PnLResult, error) {
	var positions []*Position
	var currentPosition *Position

	for _, order := range orders {
		isBuy := order.BuyToken.Mint == targetMint
		isSell := order.SellToken.Mint == targetMint

		if !isBuy && !isSell {
			continue // 跳过不涉及目标代币的交易
		}

		// 解析数量和价格（改用Amount和Decimals计算，避免依赖UiAmountString）
		amount, err := parseTokenAmount(order, isBuy)
		if err != nil {
			return nil, err
		}

		usdValue, _, err := s.getTokenUSDValue(ctx, order, isBuy, amount)
		if err != nil {
			return nil, err
		}

		// 初始化新持仓（如果当前没有持仓且是买入操作）
		if currentPosition == nil && isBuy {
			currentPosition = &Position{
				TotalAmount:     0,
				TotalCostUSD:    0,
				RealizedPnL:     0,
				TotalInvestment: 0, // 总投入成本（累计）
				TotalQuantity:   0, // 总数量（累计）
				AverageCost:     0, // 平均成本（初始为0）
				IsClosed:        false,
			}
		}

		// 处理买入：更新总投入、总数量和平均成本
		if isBuy && currentPosition != nil {
			currentPosition.TotalAmount += amount
			currentPosition.TotalCostUSD += usdValue
			// 累计总投入和总数量（用于计算历史平均成本）
			currentPosition.TotalInvestment += usdValue
			currentPosition.TotalQuantity += amount
			// 重新计算平均成本（总投入 / 总数量）
			currentPosition.AverageCost = currentPosition.TotalInvestment / currentPosition.TotalQuantity
			currentPosition.Transactions = append(currentPosition.Transactions, order)
		}

		// 处理卖出：平均成本不变（基于历史总投入和总数量）
		if isSell && currentPosition != nil {
			// 平均成本使用历史计算值（不随卖出变化）
			averageCost := currentPosition.AverageCost

			// 计算此次卖出的实现盈亏
			realized := usdValue - (amount * averageCost)
			currentPosition.RealizedPnL += realized

			// 更新当前持仓（仅减少数量和成本，不改变历史总投入/数量）
			currentPosition.TotalAmount -= amount
			currentPosition.TotalCostUSD -= amount * averageCost
			currentPosition.Transactions = append(currentPosition.Transactions, order)

			// 如果持仓数量为0，标记为已平仓并添加到持仓列表
			if currentPosition.TotalAmount <= 0 {
				currentPosition.IsClosed = true
				positions = append(positions, currentPosition)
				currentPosition = nil
			}
		}
	}

	// 添加最后未平仓的持仓
	if currentPosition != nil {
		positions = append(positions, currentPosition)
	}

	// 计算每个持仓的PnL结果
	return s.calculatePositionPnL(ctx, positions, targetMint)
}

// 辅助函数：解析代币数量（改用Amount和Decimals计算，更可靠）
func parseTokenAmount(order Order, isBuy bool) (float64, error) {
	var tokenAmount rpc.UiTokenAmount
	if isBuy {
		tokenAmount = order.BuyToken.UiTokenAmount
	} else {
		tokenAmount = order.SellToken.UiTokenAmount
	}

	// 用原始Amount和Decimals计算实际数量（避免UiAmountString的格式问题）
	amountInt, err := strconv.ParseInt(tokenAmount.Amount, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("解析数量失败: %w", err)
	}
	decimals := float64(tokenAmount.Decimals)
	return float64(amountInt) / math.Pow(10, decimals), nil
}

// calculatePositionPnL 计算每个持仓的PnL结果（修正百分比计算和格式）
func (s *PnlService) calculatePositionPnL(ctx context.Context, positions []*Position, targetMint string) ([]PnLResult, error) {
	var results []PnLResult

	// 获取当前代币价格（用于计算未实现盈亏）
	currentPrice, err := s.getCurrentTokenPrice(ctx, targetMint)
	if err != nil {
		return nil, err
	}

	for _, pos := range positions {
		// 平均成本：即使平仓（TotalAmount=0），仍使用历史计算值（最大9位小数）
		averageCost := truncateToDecimals(pos.AverageCost, 9)

		// 已实现盈亏百分比：（已实现盈亏 / 总投入成本）* 100（保留两位小数）
		var pnlPercentage float64
		if pos.TotalInvestment > 0 {
			pnlPercentage = (pos.RealizedPnL / pos.TotalInvestment) * 100
		}

		// 已实现盈亏值：保留两位小数
		profitLossValue := truncateToDecimals(pos.RealizedPnL, 10)

		// 未实现盈亏：持仓中按当前价格计算，平仓后为0（保留两位小数）
		var unrealizedProfitLossValue float64
		if !pos.IsClosed {
			unrealized := pos.TotalAmount*currentPrice - pos.TotalCostUSD
			unrealizedProfitLossValue = truncateToDecimals(unrealized, 2)
		} else {
			unrealizedProfitLossValue = 0 // 平仓后无未实现盈亏
		}

		// 格式化结果
		result := PnLResult{
			AverageCost:               averageCost,
			ProfitLossPercentage:      fmt.Sprintf("%.2f%%", pnlPercentage),
			ProfitLossValue:           profitLossValue,
			UnrealizedProfitLossValue: unrealizedProfitLossValue,
			IsClosed:                  pos.IsClosed,
		}

		results = append(results, result)
	}

	return results, nil
}

// 辅助函数：截断到指定小数位（不四舍五入）
func truncateToDecimals(value float64, decimals int) float64 {
	if decimals <= 0 {
		return math.Trunc(value)
	}

	shift := math.Pow(10, float64(decimals))
	return math.Trunc(value*shift) / shift
}

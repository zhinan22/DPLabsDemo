package services

import (
	"context"
	"strconv"
	"time"
)

// 辅助函数：获取代币的USD价值
func (s *PnlService) getTokenUSDValue(ctx context.Context, order Order, isBuy bool, amount float64) (float64, float64, error) {
	// 获取交易时的代币价格（这里需要实现实际的价格获取逻辑）
	// 实际应用中可能需要从价格API或Oracle获取
	var tokenMint string
	if isBuy {
		tokenMint = order.BuyToken.Mint
	} else {
		tokenMint = order.SellToken.Mint
	}

	price, err := s.getHistoricalTokenPrice(ctx, tokenMint, order.BlockTime)
	if err != nil {
		return 0, 0, err
	}

	return amount * price, price, nil
}

// 辅助函数：获取历史代币价格
func (s *PnlService) getHistoricalTokenPrice(ctx context.Context, mint string, timestamp time.Time) (float64, error) {

	latest, err := s.okxMarketClient.GetTokenHistoricalPriceByTimeLatest(ctx, mint, strconv.FormatInt(timestamp.UnixMilli(), 10))
	if err != nil {
		return 0, err
	}
	return latest[0].Close, nil
}

// 辅助函数：获取当前代币价格
func (s *PnlService) getCurrentTokenPrice(ctx context.Context, mint string) (float64, error) {
	latest, err := s.okxMarketClient.GetTokenHistoricalPriceByTimeLatest(ctx, mint, strconv.FormatInt(time.Now().UnixMilli(), 10))

	if err != nil {
		return 0, err
	}
	return latest[0].Close, nil
}

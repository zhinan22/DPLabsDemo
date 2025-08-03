package services

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/near/borsh-go"
	"sort"
	"time"
)

type Order struct {
	Signature string    // 交易签名
	Slot      uint64    // 区块slot
	BlockTime time.Time // 交易时间
	BuyToken  OrderTokenInfo
	SellToken OrderTokenInfo
}

// PnLResult PnL计算结果
type PnLResult struct {
	AverageCost               float64 `json:"averageCost"`               // 平均买入价格
	ProfitLossPercentage      string  `json:"profitLossPercentage"`      // 盈亏百分比
	ProfitLossValue           float64 `json:"profitLossValue"`           // 盈亏值(USD)
	UnrealizedProfitLossValue float64 `json:"unrealizedProfitLossValue"` // 未实现盈亏(USD) - 仅持仓中
	IsClosed                  bool    `json:"isClosed"`                  // 是否已平仓
}
type JupiterSwapEventData struct {
	Amm          solana.PublicKey
	InputMint    solana.PublicKey
	InputAmount  uint64
	OutputMint   solana.PublicKey
	OutputAmount uint64
}

type OrderTokenInfo struct {
	// Pubkey of the token's mint.
	Mint          string            `json:"mint"`
	UiTokenAmount rpc.UiTokenAmount `json:"uiTokenAmount"`
}

// GetUserJupiterOrdersByToken 获取用户在Jupiter上的订单并计算PnL
func (s *PnlService) CalculatePnL(ctx context.Context, txList []*Transaction, user, mint string) ([]PnLResult, error) {
	// 1. 获取所有相关订单
	orders, err := s.fetchJupiterOrders(ctx, txList, user, mint)
	if err != nil {
		return nil, err
	}

	// 2. 按时间排序（从旧到新）
	sort.Slice(orders, func(i, j int) bool {
		return orders[i].BlockTime.Before(orders[j].BlockTime)
	})

	// 3. 计算PnL
	pnlResults, err := s.calculatePnL(ctx, orders, mint)
	if err != nil {
		return nil, err
	}

	return pnlResults, nil
}

func (s *PnlService) fetchJupiterOrders(ctx context.Context, txList []*Transaction, user, mint string) ([]Order, error) {
	var orders []Order
	if len(txList) == 0 {
		return make([]Order, 0), nil
	}

	for _, tx := range txList {
		fullAccountKeys, err := GetFullAccountKeys(tx.RawTx)
		if err != nil {
			err = fmt.Errorf("err %w", err)
			continue
		}

		insTree, err := ParseInstructionTreeByStackHeight(tx.RawTx)
		if err != nil {
			err = fmt.Errorf("err %w", err)
			continue
		}
		route, event := FindNodesByProgramID(fullAccountKeys, insTree, s.jupiterPID)

		if len(route) > 1 {
			fmt.Printf("交易有一个以上jupiter %s\n", tx.Signature)
			err = fmt.Errorf("交易有一个以上jupiter %s", tx.Signature)
			continue
		}

		if len(route) == 0 {
			continue
		}

		_, tokenChangeMap, err := GetBalanceChanges(tx.RawTx, fullAccountKeys)
		if err != nil {
			return nil, err
		}

		var buyTokenMint, sellTokenMint string
		//指令对应的买卖token不准，所以改用事件 取第一个事件的input作为sellTokenMint，最后一个事件的outmint作为buyTokenMint
		//sellTokenMint = fullAccountKeys[route[0].Accounts[13]]
		//buyTokenMint = fullAccountKeys[route[0].Accounts[5]]

		for i, node := range event {
			var data JupiterSwapEventData
			if i == 0 {
				err := borsh.Deserialize(&data, node.Data[16:])
				if err != nil {
					return nil, fmt.Errorf("DecodeJupiter Deserialize(JupiterSwapEventData) %s %w", hex.EncodeToString(node.Data), err)
				}
				sellTokenMint = data.InputMint.String()
			}
			if i == len(event)-1 {
				err := borsh.Deserialize(&data, node.Data[16:])
				if err != nil {
					return nil, fmt.Errorf("DecodeJupiter Deserialize(JupiterSwapEventData) %s %w", hex.EncodeToString(node.Data), err)
				}
				buyTokenMint = data.OutputMint.String()
			}
		}

		if buyTokenMint == "So11111111111111111111111111111111111111112" {
			buyTokenMint = "SOL"
		}
		if sellTokenMint == "So11111111111111111111111111111111111111112" {
			sellTokenMint = "SOL"
		}

		if sellTokenMint == mint {
			sellTokenChange := tokenChangeMap[user][mint]
			sellToken := OrderTokenInfo{
				Mint: sellTokenMint,
				UiTokenAmount: rpc.UiTokenAmount{
					Amount:         sellTokenChange.Amount,
					Decimals:       sellTokenChange.Decimals,
					UiAmountString: sellTokenChange.UiAmountString,
				},
			}

			buyTokenChange := tokenChangeMap[user][buyTokenMint]
			buyToken := OrderTokenInfo{
				Mint: buyTokenMint,
				UiTokenAmount: rpc.UiTokenAmount{
					Amount:         buyTokenChange.Amount,
					Decimals:       buyTokenChange.Decimals,
					UiAmountString: buyTokenChange.UiAmountString,
				},
			}
			newOrder := Order{
				Signature: tx.Signature,
				Slot:      tx.Slot,
				BlockTime: tx.BlockTime,
				SellToken: sellToken,
				BuyToken:  buyToken,
			}

			orders = append(orders, newOrder)
		} else if buyTokenMint == mint {
			buyTokenChange := tokenChangeMap[user][mint]
			buyToken := OrderTokenInfo{
				Mint: buyTokenMint,
				UiTokenAmount: rpc.UiTokenAmount{
					Amount:         buyTokenChange.Amount,
					Decimals:       buyTokenChange.Decimals,
					UiAmountString: buyTokenChange.UiAmountString,
				},
			}

			sellTokenChange := tokenChangeMap[user][buyTokenMint]
			sellToken := OrderTokenInfo{
				Mint: sellTokenMint,
				UiTokenAmount: rpc.UiTokenAmount{
					Amount:         sellTokenChange.Amount,
					Decimals:       sellTokenChange.Decimals,
					UiAmountString: sellTokenChange.UiAmountString,
				},
			}
			newOrder := Order{
				Signature: tx.Signature,
				Slot:      tx.Slot,
				BlockTime: tx.BlockTime,
				SellToken: sellToken,
				BuyToken:  buyToken,
			}

			orders = append(orders, newOrder)
		}
	}
	return orders, nil

}

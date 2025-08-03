package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// JupiterTransaction 存储与Jupiter相关的交易关键信息
type Transaction struct {
	Signature string                    // 交易签名
	Slot      uint64                    // 区块slot
	BlockTime time.Time                 // 交易时间
	RawTx     *rpc.GetTransactionResult // 原始交易数据（供后续解析）
}

type PnlService struct {
	rpcClient       *rpc.Client
	jupiterPID      solana.PublicKey // Jupiter程序ID
	okxMarketClient OKXClient
	batchSize       int                     // 批量查询大小（建议50-100）
	concurrency     int                     // 并发数（批量接口不可用时使用）
	useBatchAPI     bool                    // 是否使用批量交易查询API
	cache           map[string]*Transaction // 交易缓存
	cacheMutex      sync.RWMutex
}

// NewPnlService 创建新的Solana服务实例
func NewPnlService(rpcURL string, jupiterProgramID string, config OKXClient) (*PnlService, error) {
	pid, _ := solana.PublicKeyFromBase58(jupiterProgramID)

	cache := make(map[string]*Transaction)

	return &PnlService{
		rpcClient:       rpc.New(rpcURL),
		jupiterPID:      pid,
		okxMarketClient: config,
		batchSize:       50,
		concurrency:     100,
		cache:           cache,
	}, nil
}

// GetJupiterTransactions 获取用户与Jupiter交互的交易（包含关键信息）
func (s *PnlService) GetTransactions(ctx context.Context, userAddress string, limit int) ([]*Transaction, error) {
	signatures, err := s.getPaginatedSignatures(ctx, userAddress, limit)
	if err != nil {
		return nil, fmt.Errorf("获取交易签名失败: %w", err)
	}
	if len(signatures) == 0 {
		return nil, nil
	}

	// 2. 批量获取交易详情（核心优化点）
	transactions, err := s.getBatchTransactions(ctx, signatures)
	if err != nil {
		return nil, fmt.Errorf("批量获取交易失败: %w", err)
	}

	// 按时间排序交易
	sortTransactionsByTime(transactions)

	return transactions, nil
}

// 按时间排序交易
func sortTransactionsByTime(txs []*Transaction) {
	for i := 0; i < len(txs); i++ {
		for j := i + 1; j < len(txs); j++ {
			if txs[j].BlockTime.Before(txs[i].BlockTime) {
				txs[i], txs[j] = txs[j], txs[i]
			} else if txs[j].BlockTime.Equal(txs[i].BlockTime) && txs[j].Slot < txs[i].Slot {
				// 时间相同则按slot排序
				txs[i], txs[j] = txs[j], txs[i]
			}
		}
	}
}

// checkAndExtractJupiterTx 验证是否为Jupiter交易，并提取关键信息
func (s *PnlService) getTransactions(ctx context.Context, signature solana.Signature) (*Transaction, bool, error) {
	maxVersion := uint64(0)
	// 获取原始交易数据
	rawTx, err := s.rpcClient.GetTransaction(ctx, signature, &rpc.GetTransactionOpts{
		MaxSupportedTransactionVersion: &maxVersion,
	})
	if err != nil {
		return nil, false, fmt.Errorf("获取交易详情失败: %w", err)
	}

	// 处理时间
	var blockTime time.Time
	if rawTx.BlockTime != nil {
		blockTime = time.Unix(int64(*rawTx.BlockTime), 0)
	} else {
		blockTime = time.Time{}
	}

	// 封装交易信息，使用修改后的交易数据
	txInfo := &Transaction{
		Signature: signature.String(),
		Slot:      rawTx.Slot,
		BlockTime: blockTime,
		RawTx:     rawTx,
	}

	return txInfo, true, nil
}

func (s *PnlService) getPaginatedSignatures(ctx context.Context, user string, limit int) ([]solana.Signature, error) {
	userAddr, err := solana.PublicKeyFromBase58(user)
	if err != nil {
		return nil, err
	}

	var allSignatures []solana.Signature
	var before solana.Signature
	pageSize := s.batchSize

	for len(allSignatures) < limit {
		// 计算当前页需要的数量
		remaining := limit - len(allSignatures)
		if pageSize > remaining {
			pageSize = remaining
		}

		// 获取一页签名
		sigs, err := s.rpcClient.GetSignaturesForAddressWithOpts(
			ctx,
			userAddr,
			&rpc.GetSignaturesForAddressOpts{
				Limit:      &pageSize,
				Before:     before,
				Commitment: rpc.CommitmentFinalized,
			},
		)
		if err != nil {
			return nil, err
		}
		if len(sigs) == 0 {
			break // 没有更多签名
		}

		// 提取签名
		for _, sig := range sigs {
			allSignatures = append(allSignatures, sig.Signature)
		}

		// 准备下一页
		before = sigs[len(sigs)-1].Signature
		if len(sigs) < pageSize {
			break // 最后一页
		}
	}

	return allSignatures, nil
}

func (s *PnlService) getBatchTransactions(ctx context.Context, signatures []solana.Signature) ([]*Transaction, error) {
	// 先查缓存
	cached, remaining := s.getCachedTransactions(signatures)
	if len(remaining) == 0 {
		return cached, nil
	}
	var newTransactions []*Transaction
	var err error

	newTransactions, err = s.concurrentGetTransactions(ctx, remaining)
	if err != nil {
		return nil, err
	}

	// 缓存结果
	s.cacheTransactions(newTransactions)

	return append(cached, newTransactions...), nil
}

//// batchGetTransactions 使用批量API获取交易
//func (s *PnlService) batchGetTransactions(ctx context.Context, signatures []solana.Signature) ([]*rpc.GetTransactionResult, error) {
//	var allResults []*rpc.GetTransactionResult
//
//	// 分批次处理（避免单次请求过大）
//	for i := 0; i < len(signatures); i += s.batchSize {
//		end := i + s.batchSize
//		if end > len(signatures) {
//			end = len(signatures)
//		}
//		batch := signatures[i:end]
//		results, err := s.rpcClient.GetTransaction(
//			ctx,
//			batch,
//			rpc.CommitmentFinalized,
//		)
//		if err != nil {
//			return nil, err
//		}
//
//		// 验证结果数量
//		if len(results) != len(batch) {
//			return nil, errors.New("批量查询结果数量不匹配")
//		}
//
//		allResults = append(allResults, results...)
//	}
//
//	return allResults, nil
//}

// 缓存相关方法
func (s *PnlService) getCachedTransactions(signatures []solana.Signature) ([]*Transaction, []solana.Signature) {
	s.cacheMutex.RLock()
	defer s.cacheMutex.RUnlock()

	var cached []*Transaction
	var remaining []solana.Signature

	for _, sig := range signatures {
		key := sig.String()
		if tx, ok := s.cache[key]; ok {
			cached = append(cached, tx)
		} else {
			remaining = append(remaining, sig)
		}
	}

	return cached, remaining
}

func (s *PnlService) cacheTransactions(transactions []*Transaction) {
	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()

	for _, tx := range transactions {
		if tx != nil {
			s.cache[tx.Signature] = tx
		}
	}
}

// concurrentGetTransactions
func (s *PnlService) concurrentGetTransactions(ctx context.Context, signatures []solana.Signature) ([]*Transaction, error) {
	resultChan := make(chan struct {
		index int
		tx    *Transaction
		err   error
	}, len(signatures))
	var wg sync.WaitGroup

	// 控制并发数
	semaphore := make(chan struct{}, len(signatures))

	for i, sig := range signatures {
		wg.Add(1)
		go func(idx int, signature solana.Signature) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			var zero uint64 = 0

			// 使用单个查询方法
			rawTx, err := s.rpcClient.GetTransaction(
				ctx,
				signature,
				&rpc.GetTransactionOpts{
					Commitment:                     rpc.CommitmentFinalized,
					MaxSupportedTransactionVersion: &zero,
				},
			)

			if err != nil {
				resultChan <- struct {
					index int
					tx    *Transaction
					err   error
				}{idx, nil, fmt.Errorf("获取交易 %s 失败: %w", signature, err)}
				return
			}

			// 转换为自定义Transaction结构体时修复时间转换
			var blockTime time.Time
			if rawTx.BlockTime != nil {
				// 解引用UnixTimeSeconds指针获取int64值
				blockTime = time.Unix(int64(*rawTx.BlockTime), 0)
			} else {
				// 处理BlockTime为nil的情况（极少数）
				blockTime = time.Time{}
			}
			tx := &Transaction{
				Signature: signature.String(),
				Slot:      rawTx.Slot,
				BlockTime: blockTime,
				RawTx:     rawTx,
			}

			resultChan <- struct {
				index int
				tx    *Transaction
				err   error
			}{idx, tx, nil}
		}(i, sig)
	}

	// 等待所有请求完成
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// 收集结果并按原始顺序排列
	results := make([]*Transaction, len(signatures))
	var firstErr error

	for res := range resultChan {
		if res.tx.Signature == "487tDZsjDh7jZ3DzAfZXZE699Pt22DsRRMZ7pRqHAHNaxkcc41rBjnsoTZQYoV983XnkR5rxn5tDdNWTL5V41AVu" {
			fmt.Printf("找到了22")
		}
		if res.err != nil && firstErr == nil {
			firstErr = res.err
		}
		if res.tx != nil {
			results[res.index] = res.tx
		}
	}

	if firstErr != nil {
		return nil, firstErr
	}

	return results, nil
}

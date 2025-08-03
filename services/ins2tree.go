package services

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"math/big"
	"sort"
	"strconv"
	"strings"
)

// StackInstructionNode 基于栈高度的指令节点
type StackInstructionNode struct {
	Index          int                     // 指令在交易中的原始索引
	StackHeight    uint64                  // 栈高度，用于确定层级
	ProgramIDIndex uint16                  // 程序ID
	Data           []byte                  // 指令数据
	Accounts       []uint16                // 涉及的账户
	Parent         *StackInstructionNode   // 父节点
	Children       []*StackInstructionNode // 子节点
	InnerIndexes   []int                   // 包含的内部指令索引
}

// 指令信息及对应的栈高度
type indexedInstruction struct {
	Index          int      // 指令在全局列表中的索引
	ParentTopIndex int      // 所属的顶层指令索引（用于关联父节点）
	StackHeight    uint64   // 栈高度
	ProgramIDIndex uint16   // 程序ID索引
	Data           []byte   // 指令数据
	Accounts       []uint16 // 账户索引列表
}

type TokenInfo struct {
	Owner    string
	Mint     string
	Decimals uint8
}

// getAllInstructionsWithStackHeight 修复内部指令与顶层指令的关联关系
func getAllInstructionsWithStackHeight(tx *rpc.GetTransactionResult) ([]indexedInstruction, error) {
	transactions, err := tx.Transaction.GetTransaction()
	if err != nil {
		return nil, fmt.Errorf("解析交易消息失败: %w", err)
	}
	message := transactions.Message
	allInstr := []indexedInstruction{}

	// 1. 添加顶层指令（栈高度为0）
	topLevelInstrs := message.Instructions
	for i, ix := range topLevelInstrs {
		allInstr = append(allInstr, indexedInstruction{
			Index:          i,  // 顶层指令索引从0开始
			ParentTopIndex: -1, // 顶层指令无父顶层指令
			StackHeight:    0,  // 顶层指令栈高度为0
			ProgramIDIndex: ix.ProgramIDIndex,
			Data:           ix.Data,
			Accounts:       ix.Accounts,
		})
	}

	// 2. 添加内部指令（子指令），关联到所属的顶层指令
	if tx.Meta != nil && tx.Meta.InnerInstructions != nil {
		for _, inner := range tx.Meta.InnerInstructions {
			// inner.Index 表示这些内部指令所属的顶层指令索引（关键关联！）
			parentTopIndex := int(inner.Index)
			if parentTopIndex < 0 || parentTopIndex >= len(topLevelInstrs) {
				return nil, fmt.Errorf("内部指令所属的顶层指令索引%d无效", parentTopIndex)
			}

			// 父顶层指令的栈高度为0，子指令栈高度 = 父栈高度 + 1（可根据实际情况调整）
			parentStackHeight := allInstr[parentTopIndex].StackHeight
			childStackHeight := parentStackHeight + 1

			// 遍历内部指令，添加到全局列表
			for _, ix := range inner.Instructions {
				allInstr = append(allInstr, indexedInstruction{
					Index:          len(allInstr),    // 全局索引自增
					ParentTopIndex: parentTopIndex,   // 关联到所属的顶层指令
					StackHeight:    childStackHeight, // 子指令栈高度 = 父+1
					ProgramIDIndex: ix.ProgramIDIndex,
					Data:           ix.Data,
					Accounts:       ix.Accounts,
				})
			}
		}
	}

	return allInstr, nil
}

// ParseInstructionTreeByStackHeight 基于修复后的指令列表构建指令树
func ParseInstructionTreeByStackHeight(tx *rpc.GetTransactionResult) (*StackInstructionNode, error) {
	if tx == nil || tx.Transaction == nil {
		return nil, fmt.Errorf("交易数据为空")
	}

	allInstructions, err := getAllInstructionsWithStackHeight(tx)
	if err != nil {
		return nil, fmt.Errorf("获取指令失败: %w", err)
	}

	if len(allInstructions) == 0 {
		return nil, fmt.Errorf("交易不包含任何指令")
	}

	// 按全局索引排序（确保执行顺序）
	sort.Slice(allInstructions, func(i, j int) bool {
		return allInstructions[i].Index < allInstructions[j].Index
	})

	// 构建节点映射（索引 -> 节点），方便快速查找父节点
	nodeMap := make(map[int]*StackInstructionNode)
	rootNodes := []*StackInstructionNode{}

	for _, instr := range allInstructions {
		node := &StackInstructionNode{
			Index:          instr.Index,
			StackHeight:    instr.StackHeight,
			ProgramIDIndex: instr.ProgramIDIndex,
			Data:           instr.Data,
			Accounts:       instr.Accounts,
			Children:       []*StackInstructionNode{},
		}
		nodeMap[instr.Index] = node

		// 关联父节点
		if instr.ParentTopIndex == -1 {
			// 顶层指令：无父节点，加入根节点列表
			rootNodes = append(rootNodes, node)
		} else {
			// 子指令：父节点是其所属的顶层指令（或该顶层指令的子指令，根据栈高度）
			// 1. 先找到所属的顶层指令节点
			parentTopNode, exists := nodeMap[instr.ParentTopIndex]
			if !exists {
				return nil, fmt.Errorf("子指令%d的父顶层指令%d不存在", instr.Index, instr.ParentTopIndex)
			}

			// 2. 根据栈高度找到具体父节点（栈高度-1的节点）
			// 从父顶层指令开始，查找栈高度为当前栈高度-1的最近节点
			parentNode := findParentByStackHeight(parentTopNode, instr.StackHeight-1)
			if parentNode == nil {
				return nil, fmt.Errorf("子指令%d找不到栈高度为%d的父节点", instr.Index, instr.StackHeight-1)
			}

			// 3. 建立父子关系
			node.Parent = parentNode
			parentNode.Children = append(parentNode.Children, node)
		}
	}

	// 处理多根节点情况
	if len(rootNodes) > 1 {
		virtualRoot := &StackInstructionNode{
			Index:       -1,
			StackHeight: 0,
			Children:    rootNodes,
		}
		for _, root := range rootNodes {
			root.Parent = virtualRoot
		}
		return virtualRoot, nil
	}

	return rootNodes[0], nil
}

// 辅助函数：从某个节点开始，查找栈高度为targetHeight的最近父节点
func findParentByStackHeight(startNode *StackInstructionNode, targetHeight uint64) *StackInstructionNode {
	// 从startNode开始向上（包括自身）查找栈高度匹配的节点
	current := startNode
	for current != nil {
		if current.StackHeight == targetHeight {
			return current
		}
		current = current.Parent
	}
	// 向上找不到则从startNode的子节点中查找
	return findChildByStackHeight(startNode, targetHeight)
}

// 辅助函数：从某个节点的子树中查找栈高度为targetHeight的节点
func findChildByStackHeight(node *StackInstructionNode, targetHeight uint64) *StackInstructionNode {
	if node.StackHeight == targetHeight {
		return node
	}
	for _, child := range node.Children {
		found := findChildByStackHeight(child, targetHeight)
		if found != nil {
			return found
		}
	}
	return nil
}

// PrintStackInstructionTree 打印基于栈高度的指令树
func PrintStackInstructionTree(node *StackInstructionNode, indent int) {
	if node == nil {
		return
	}

	indentStr := ""
	for i := 0; i < indent; i++ {
		indentStr += "  "
	}

	programID := node.ProgramIDIndex
	if node.Index == -1 {
		programID = 0
	}

	fmt.Printf("%s指令#%d (栈高度: %d) - 程序: %d\n", indentStr, node.Index, node.StackHeight, programID)
	fmt.Printf("%s  账户数: %d, 数据长度: %d字节\n", indentStr, len(node.Accounts), len(node.Data))

	// 递归打印子节点
	for _, child := range node.Children {
		PrintStackInstructionTree(child, indent+1)
	}
}

// FindNodesByProgramID 从指令树中查找所有匹配指定Program ID的节点
func FindNodesByProgramID(fullAccountKeys []solana.PublicKey, root *StackInstructionNode, targetProgramID solana.PublicKey) ([]*StackInstructionNode, []*StackInstructionNode) {
	var route []*StackInstructionNode
	var event []*StackInstructionNode

	if root == nil {
		return route, event
	}

	// 递归遍历指令树
	var traverse func(node *StackInstructionNode)
	traverse = func(node *StackInstructionNode) {
		// 检查当前节点是否匹配目标Program ID
		nodeProgram := fullAccountKeys[node.ProgramIDIndex]
		if nodeProgram.Equals(targetProgramID) {
			activeTag := hex.EncodeToString(node.Data[0:8])
			activeTag2 := hex.EncodeToString(node.Data[8:16])

			if activeTag == "e517cb977ae3ad2a" { //获得route指令
				route = append(route, node)
			}
			if activeTag == "e445a52e51cb9a1d" && activeTag2 == "40c6cde8260871e2" { //获取jupitor事件
				event = append(event, node)
			}
		}
		// 递归处理子节点
		for _, child := range node.Children {
			traverse(child)
		}
	}

	traverse(root)
	return route, event
}

func GetFullAccountKeys(tx *rpc.GetTransactionResult) ([]solana.PublicKey, error) {
	transaction, err := tx.Transaction.GetTransaction()
	if err != nil {
		return nil, err
	}
	var fullAccountKeys []solana.PublicKey
	fullAccountKeys = append(fullAccountKeys, transaction.Message.AccountKeys...)
	// 支持处理v0类型的交易，修改transaction中的AccountKeys
	if transaction.Message.GetVersion() == solana.MessageVersionLegacy {
		if len(tx.Meta.LoadedAddresses.Writable) > 0 {
			fullAccountKeys = append(fullAccountKeys,
				tx.Meta.LoadedAddresses.Writable...)
		}
		if len(tx.Meta.LoadedAddresses.ReadOnly) > 0 {
			fullAccountKeys = append(fullAccountKeys,
				tx.Meta.LoadedAddresses.ReadOnly...)
		}
	}

	return fullAccountKeys, err
}

type TokenChange struct {
	Amount         string  // 原始数量变化（字符串，支持大数字）
	Decimals       uint8   // 代币小数位数
	UiAmountString string  // 格式化后的变化值（考虑小数位数）
	UiAmount       float64 // 格式化后的变化值（浮点数）
}

// GetBalanceChanges 解析交易中所有地址的资产余额变化（SOL也作为特殊代币处理）
func GetBalanceChanges(tx *rpc.GetTransactionResult, accountKeys []solana.PublicKey) (
	map[string]*TokenInfo, // tokenMap: 代币账户地址 -> 代币信息
	map[string]map[string]*TokenChange, // unifiedChangeMap: 所有者地址 -> 资产标识(SOL或Mint) -> 变化详情
	error,
) {
	if tx == nil || tx.Meta == nil {
		return nil, nil, errors.New("交易元数据为空")
	}
	meta := tx.Meta

	// 初始化数据结构
	balanceMap := make(map[string]map[string]*rpc.UiTokenAmount)     // 地址 -> 资产标识 -> 交易后余额
	tokenMap := make(map[string]*TokenInfo)                          // 代币账户地址 -> 代币信息
	preTokenBalMap := make(map[string]map[string]*rpc.UiTokenAmount) // 所有者地址 -> 资产标识 -> 交易前余额
	accountAssetMap := make(map[string]struct{})                     // 标记交易后存在的（所有者+资产）组合
	unifiedChangeMap := make(map[string]map[string]*TokenChange)     // 统一的资产变化映射

	// 1. 处理交易前的代币余额
	for _, preTb := range meta.PreTokenBalances {
		ownerAddr := preTb.Owner.String()
		mintAddr := preTb.Mint.String()
		accountIdx := preTb.AccountIndex

		// 记录代币基本信息
		pk := accountKeys[accountIdx].String()
		tokenInfo := &TokenInfo{
			Owner:    ownerAddr,
			Mint:     mintAddr,
			Decimals: preTb.UiTokenAmount.Decimals,
		}
		tokenMap[pk] = tokenInfo

		// 记录交易前余额（只保存Amount和Decimals）
		if _, ok := preTokenBalMap[ownerAddr]; !ok {
			preTokenBalMap[ownerAddr] = make(map[string]*rpc.UiTokenAmount)
		}
		preTokenBalMap[ownerAddr][mintAddr] = &rpc.UiTokenAmount{
			Amount:   preTb.UiTokenAmount.Amount,
			Decimals: preTb.UiTokenAmount.Decimals,
		}
	}

	// 2. 处理交易前的SOL余额（作为特殊资产添加到preTokenBalMap）
	for i, preBal := range meta.PreBalances {
		ownerAddr := accountKeys[i].String()
		assetKey := "SOL" // 用"SOL"作为SOL的资产标识

		if _, ok := preTokenBalMap[ownerAddr]; !ok {
			preTokenBalMap[ownerAddr] = make(map[string]*rpc.UiTokenAmount)
		}
		preTokenBalMap[ownerAddr][assetKey] = &rpc.UiTokenAmount{
			Amount:   big.NewInt(int64(preBal)).String(),
			Decimals: 9, // SOL固定9位小数
		}
	}

	// 3. 处理交易后的代币余额
	for _, postTb := range meta.PostTokenBalances {
		ownerAddr := postTb.Owner.String()
		mintAddr := postTb.Mint.String()
		accountIdx := postTb.AccountIndex

		// 记录代币基本信息
		pk := accountKeys[accountIdx].String()
		tokenInfo := &TokenInfo{
			Owner:    ownerAddr,
			Mint:     mintAddr,
			Decimals: postTb.UiTokenAmount.Decimals,
		}
		tokenMap[pk] = tokenInfo

		// 标记交易后存在的（所有者+资产）组合
		assetKey := fmt.Sprintf("%s_%s", ownerAddr, mintAddr)
		accountAssetMap[assetKey] = struct{}{}

		// 记录交易后代币余额（只保存Amount和Decimals）
		if _, ok := balanceMap[ownerAddr]; !ok {
			balanceMap[ownerAddr] = make(map[string]*rpc.UiTokenAmount)
		}
		balanceMap[ownerAddr][mintAddr] = &rpc.UiTokenAmount{
			Amount:   postTb.UiTokenAmount.Amount,
			Decimals: postTb.UiTokenAmount.Decimals,
		}
	}

	// 4. 处理交易后的SOL余额（作为特殊代币"SOL"处理）
	for i, postBal := range meta.PostBalances {
		ownerAddr := accountKeys[i].String()
		assetKey := "SOL"

		// 标记交易后存在的SOL资产
		solAssetKey := fmt.Sprintf("%s_%s", ownerAddr, assetKey)
		accountAssetMap[solAssetKey] = struct{}{}

		if _, ok := balanceMap[ownerAddr]; !ok {
			balanceMap[ownerAddr] = make(map[string]*rpc.UiTokenAmount)
		}
		balanceMap[ownerAddr][assetKey] = &rpc.UiTokenAmount{
			Amount:   big.NewInt(int64(postBal)).String(),
			Decimals: 9,
		}
	}

	// 5. 补充交易后余额为0的资产（交易前有，交易后无）
	for ownerAddr, preAssets := range preTokenBalMap {
		for assetID, preTa := range preAssets {
			assetKey := fmt.Sprintf("%s_%s", ownerAddr, assetID)
			if _, exists := accountAssetMap[assetKey]; !exists {
				// 交易后该资产余额为0
				if _, ok := balanceMap[ownerAddr]; !ok {
					balanceMap[ownerAddr] = make(map[string]*rpc.UiTokenAmount)
				}
				balanceMap[ownerAddr][assetID] = &rpc.UiTokenAmount{
					Decimals: preTa.Decimals,
					Amount:   "0",
				}
			}
		}
	}

	// 6. 计算所有资产余额变化，构建统一的变化映射
	for ownerAddr, postAssets := range balanceMap {
		for assetID, postTa := range postAssets {
			// 初始化所有者的变化映射
			if _, ok := unifiedChangeMap[ownerAddr]; !ok {
				unifiedChangeMap[ownerAddr] = make(map[string]*TokenChange)
			}

			// 获取交易前余额
			var preAmount *big.Int
			var decimals uint8 = postTa.Decimals

			if preOwnerAssets, ok := preTokenBalMap[ownerAddr]; ok {
				if preTa, ok := preOwnerAssets[assetID]; ok {
					preAmt, _ := new(big.Int).SetString(preTa.Amount, 10)
					preAmount = preAmt
					decimals = preTa.Decimals
				}
			}
			if preAmount == nil {
				preAmount = big.NewInt(0)
			}

			// 计算变化量（原始数量）
			postAmount, _ := new(big.Int).SetString(postTa.Amount, 10)
			changeAmount := new(big.Int).Sub(postAmount, preAmount)
			absoluteChange := new(big.Int).Abs(changeAmount) // 取绝对值

			// 格式化变化值
			uiAmountString := formatTokenAmount(absoluteChange.String(), decimals)

			// 记录变化信息（标记是否为SOL）
			unifiedChangeMap[ownerAddr][assetID] = &TokenChange{
				Amount:         absoluteChange.String(),
				Decimals:       decimals,
				UiAmountString: uiAmountString,
			}
		}
	}

	return tokenMap, unifiedChangeMap, nil
}

// formatTokenAmount 将原始数量字符串按指定小数位数格式化为可读字符串
// 例如: formatTokenAmount("123456", 6) -> "0.123456"
//
//	formatTokenAmount("123456789", 6) -> "123.456789"
//	formatTokenAmount("-12345", 2) -> "-0.12345" (注意这里小数位数不足时补零)
func formatTokenAmount(amountStr string, decimals uint8) string {
	if amountStr == "" {
		return "0"
	}

	// 处理负数
	negative := false
	if amountStr[0] == '-' {
		negative = true
		amountStr = amountStr[1:]
	}

	// 补零确保长度足够
	requiredLen := int(decimals) + 1
	if len(amountStr) < requiredLen {
		amountStr = fmt.Sprintf("%0"+strconv.Itoa(requiredLen)+"s", amountStr)
		amountStr = strings.ReplaceAll(amountStr, " ", "0")
	}

	// 分割整数和小数部分
	integerPart := amountStr[:len(amountStr)-int(decimals)]
	fractionalPart := amountStr[len(amountStr)-int(decimals):]

	// 移除整数部分前导零
	integerPart = strings.TrimLeft(integerPart, "0")
	if integerPart == "" {
		integerPart = "0"
	}

	// 组合结果
	result := fmt.Sprintf("%s.%s", integerPart, fractionalPart)

	// 添加负号
	if negative {
		result = "-" + result
	}

	return result
}

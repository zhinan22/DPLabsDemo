# DPLabsDemo 简介

## 基本信息

开发语言 go 

版本 1.24.3

标准库 + Gin（Web 框架） github.com/gin-gonic/gin v1.10.1

Solana sdk : github.com/gagliardetto/solana-go v1.13.0

## 逻辑

用户交易 PnL 计算流程

1. **用户输入与请求接收**

   - 用户输入目标代币地址（`token地址`）和用户钱包地址（`用户地址`）
   - 系统接收请求并触发处理流程

2. **获取用户交易数据**

   - 调用链上接口，获取该用户最近的交易记录（默认取最近 100 条）
   - 并发调用get transaction

3. **交易指令树解析**

   - 遍历每笔交易，按以下规则解析为树状结构：
     - 根据`stackHeight`（栈高度）区分指令层级
     - 通过子指令对应的`index`关联父指令，构建完整指令树

4. **Jupiter 交易识别与关键信息提取**

   - 遍历指令树，判断是否包含Jupiter route

     和event 相关指令：

     - 若不包含，跳过该交易，继续处理下一笔
     - 若包含，解析event数组：
       - 取**第一个 event 的 input**作为`sellMint`（卖出代币地址）
       - 取**最后一个 event 的 outMint**作为`buyMint`（买入代币地址）

5. **订单信息生成**

   - 结合交易记录中的`token变化数据`（余额变动），生成该笔交易对应的`Jupiter订单（Order）`

6. **批量处理与汇总**

   - 重复步骤 3-5，处理所有 所有交易，汇总所有符合条件的`Jupiter订单`
     按时间先后排序

7. **PnL 计算**

   - 基于汇总的Order

     列表，按持仓平均成本法计算盈亏（PnL）：

     - 统计买入 / 卖出数量、成本与收入
     - 计算已实现盈亏、未实现盈亏及盈亏百分比

8. **结果返回**

   - 将计算完成的 PnL 结果返回给用户

本地启动
go mod tidt
go run main.go
curl "http://localhost:8080/pnl?userAddress=DxhVG5CzS5GHWkpZKtnGYYAsmUbE7FgdYbMYK6FGQ8hP&tokenMint=6p6xgHyF7AeE6TZkSmFsko444wqoP15icUSqi2jfGiPN&limit=10"



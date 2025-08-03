package config

import (
	"github.com/zhinan22/DPLabsDemo/services"
	"os"
	"strconv"
)

// Config 应用配置
type Config struct {
	SolanaRPCUrl     string
	JupiterProgramID string
	ServerPort       string
	TransactionLimit int
	OKXClient        services.OKXClient
}

// LoadConfig 从环境变量加载配置
func LoadConfig() (Config, error) {
	// 默认值
	transactionLimit := 1000
	if val, exists := os.LookupEnv("TRANSACTION_LIMIT"); exists {
		parsed, err := strconv.Atoi(val)
		if err == nil {
			transactionLimit = parsed
		}
	}

	port := "8080"
	if val, exists := os.LookupEnv("PORT"); exists {
		port = val
	}

	// 初始化OKX配置，从环境变量读取或使用默认值
	var OKXClientInstance = services.OKXClient{
		BaseUrl:              getEnv("BASEURL", "https://web3.okx.com"),
		MarketHistoricalPath: getEnv("MARKET_HISTORICAL_PATH", "/api/v5/market/historical-candles"),
		MarketCurrentPath:    getEnv("MARKET_CURRENT_PATH", "/api/v5/market/price-info"),
		ApiKey:               getEnv("API_KEY", ""),
		PassPhrase:           getEnv("PASS_PHRASE", ""),
		SecretKey:            getEnv("SECRET_KEY", ""),
	}
	return Config{
		SolanaRPCUrl:     getEnv("SOLANA_RPC_URL", "https://api.mainnet-beta.solana.com"),
		JupiterProgramID: getEnv("JUPITER_PROGRAM_ID", "JUP6LkbZbjS1jKKwapdHNy74zcZ3tLUZoi5QNyVTaV4"),
		ServerPort:       port,
		OKXClient:        OKXClientInstance,
		TransactionLimit: transactionLimit,
	}, nil
}

// getEnv 获取环境变量或返回默认值
func getEnv(key, defaultValue string) string {
	if val, exists := os.LookupEnv(key); exists {
		return val
	}
	return defaultValue
}

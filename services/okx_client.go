package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"time"
)

type OKXClient struct {
	BaseUrl              string
	MarketHistoricalPath string
	MarketCurrentPath    string
	ApiKey               string
	PassPhrase           string
	SecretKey            string
}

type OKXTokenPriceRequest struct {
	ChainIndex           string `param:"chainIndex"`
	TokenContractAddress string `param:"tokenContractAddress"`
	after                string `param:"after"`
	before               string `param:"before"`
	bar                  string `param:"bar"`
	limit                string `param:"limit"`
}

func (r OKXTokenPriceRequest) String() string {
	params := url.Values{}
	v := reflect.ValueOf(r)
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("param")
		if tag == "" {
			continue
		}

		value := v.Field(i).String()
		if value != "" {
			params.Set(tag, value)
		}
	}

	return params.Encode()
}

// MarketResponse 对应完整的JSON响应结构
type MarketResponse struct {
	Code string     `json:"code"` // 状态码，"0"表示成功
	Msg  string     `json:"msg"`  // 消息描述
	Data [][]string `json:"data"` // 行情数据数组
}

// MarketRecord 解析后的的单条行情记录
type MarketRecord struct {
	Timestamp  time.Time // 时间戳（转换后）
	Open       float64   // 开盘价
	High       float64   // 最高价
	Low        float64   // 最低价
	Close      float64   // 收盘价
	Volume     float64   // 成交量
	Turnover   float64   // 成交额
	IsComplete int       // 数据完整性标识
}

// ParseRecords 将原始响应数据解析为结构化的MarketRecord切片
func (r *MarketResponse) ParseRecords() ([]MarketRecord, error) {
	var records []MarketRecord
	for _, raw := range r.Data {
		record, err := parseRecord(raw)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

// parseRecord 解析单条原始字符串数组为MarketRecord
func parseRecord(raw []string) (MarketRecord, error) {
	// 验证数据长度
	if len(raw) != 8 {
		return MarketRecord{}, ErrInvalidRecordLength
	}

	// 解析时间戳（毫秒）
	timestamp, err := parseTimestamp(raw[0])
	if err != nil {
		return MarketRecord{}, wrapError("timestamp", err)
	}

	// 解析价格字段
	open, err := parseFloat(raw[1])
	if err != nil {
		return MarketRecord{}, wrapError("open", err)
	}

	high, err := parseFloat(raw[2])
	if err != nil {
		return MarketRecord{}, wrapError("high", err)
	}

	low, err := parseFloat(raw[3])
	if err != nil {
		return MarketRecord{}, wrapError("low", err)
	}

	closePrice, err := parseFloat(raw[4])
	if err != nil {
		return MarketRecord{}, wrapError("close", err)
	}

	volume, err := parseFloat(raw[5])
	if err != nil {
		return MarketRecord{}, wrapError("volume", err)
	}

	turnover, err := parseFloat(raw[6])
	if err != nil {
		return MarketRecord{}, wrapError("turnover", err)
	}

	isComplete, err := parseInt(raw[7])
	if err != nil {
		return MarketRecord{}, wrapError("is_complete", err)
	}

	return MarketRecord{
		Timestamp:  timestamp,
		Open:       open,
		High:       high,
		Low:        low,
		Close:      closePrice,
		Volume:     volume,
		Turnover:   turnover,
		IsComplete: isComplete,
	}, nil
}

// 辅助函数：解析时间戳（毫秒 -> time.Time）
func parseTimestamp(s string) (time.Time, error) {
	millis, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(millis), nil
}

// 辅助函数：解析字符串为float64
func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}

// 辅助肋函数：解析字符串为int
func parseInt(s string) (int, error) {
	return strconv.Atoi(s)
}

// 错误处理相关定义
var (
	ErrInvalidRecordLength = errors.New("invalid record length (expected 8 fields)")
)

// wrapError 包装字段解析错误
func wrapError(field string, err error) error {
	return fmt.Errorf("parse %s failed: %w", field, err)
}

func (o OKXClient) GetTokenHistoricalPriceByTimeLatest(ctx context.Context, mint string, stime string) ([]MarketRecord, error) {
	// 构建请求参数结构体
	reqParams := OKXTokenPriceRequest{
		ChainIndex:           "501",
		TokenContractAddress: mint,
		after:                stime,
		bar:                  "1s",
	}
	// 生成时间戳（UTC格式）
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	// 构建签名内容
	method := "GET"
	signatureContent := timestamp + method + o.MarketHistoricalPath + "?" + reqParams.String()

	// 计算HMAC-SHA256签名
	h := hmac.New(sha256.New, []byte(o.SecretKey))
	h.Write([]byte(signatureContent))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	// 构建完整URL
	fullURL := fmt.Sprintf("%s%s?%s", o.BaseUrl, o.MarketHistoricalPath, reqParams.String())

	// 创建HTTP请求
	req, err := http.NewRequest(method, fullURL, nil)
	if err != nil {
		err := fmt.Errorf("OKXApprove创建请求失败", err)
		return nil, err
	}

	// 设置请求头
	req.Header.Set("OK-ACCESS-KEY", o.ApiKey)
	req.Header.Set("OK-ACCESS-SIGN", signature)
	req.Header.Set("OK-ACCESS-PASSPHRASE", o.PassPhrase)
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		err := fmt.Errorf("OKXApprove发送请求失败:", err)
		return nil, err
	}
	defer resp.Body.Close()

	// 读取响应内容
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		err := fmt.Errorf("OKXApprove读取响应失败", err)
		return nil, err
	}
	// 1. 解析JSON到MarketResponse
	var response MarketResponse
	if err := json.Unmarshal(body, &response); err != nil {
		log.Fatalf("JSON解析失败: %v", err)
	}

	// 2. 将原始数据转换为MarketRecord切片
	records, err := response.ParseRecords()
	if err != nil {
		log.Fatalf("数据转换失败: %v", err)
	}

	return records, nil

}

func (o OKXClient) GetTokenCurrentPrice(ctx context.Context, mint string) ([]MarketRecord, error) {
	// 构建请求参数结构体
	reqParams := OKXTokenPriceRequest{
		ChainIndex:           "501",
		TokenContractAddress: mint,
	}
	// 生成时间戳（UTC格式）
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	// 构建签名内容
	method := "GET"
	signatureContent := timestamp + method + o.MarketCurrentPath + "?" + reqParams.String()

	// 计算HMAC-SHA256签名
	h := hmac.New(sha256.New, []byte(o.SecretKey))
	h.Write([]byte(signatureContent))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	// 构建完整URL
	fullURL := fmt.Sprintf("%s%s?%s", o.BaseUrl, o.MarketCurrentPath, reqParams.String())
	fmt.Printf("url== %s\n", fullURL)

	// 创建HTTP请求
	req, err := http.NewRequest(method, fullURL, nil)
	if err != nil {
		err := fmt.Errorf("OKXApprove创建请求失败", err)
		return nil, err
	}

	// 设置请求头
	req.Header.Set("OK-ACCESS-KEY", o.ApiKey)
	req.Header.Set("OK-ACCESS-SIGN", signature)
	req.Header.Set("OK-ACCESS-PASSPHRASE", o.PassPhrase)
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		err := fmt.Errorf("OKXApprove发送请求失败:", err)
		return nil, err
	}
	defer resp.Body.Close()

	// 读取响应内容
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		err := fmt.Errorf("OKXApprove读取响应失败", err)
		return nil, err
	}
	// 1. 解析JSON到MarketResponse
	var response MarketResponse
	if err := json.Unmarshal(body, &response); err != nil {
		log.Fatalf("JSON解析失败: %v", err)
	}

	// 2. 将原始数据转换为MarketRecord切片
	records, err := response.ParseRecords()
	if err != nil {
		log.Fatalf("数据转换失败: %v", err)
	}

	return records, nil

}

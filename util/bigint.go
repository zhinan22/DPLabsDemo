package util

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/shopspring/decimal"
	"math/big"
	"strings"
	"sync"
)

var exp = sync.Map{}
var ErrInvalidNumber = errors.New("bigint: not a valid number")

type Int struct {
	*big.Int
}

func New(x int64) Int {
	data := big.NewInt(x)
	return Int{data}
}

func NewUint64(x uint64) Int {
	return Int{Int: new(big.Int).SetUint64(x)}
}

func FromDecimal(decimal string) (Int, error) {
	i, ok := new(big.Int).SetString(decimal, 10)
	if !ok {
		return Int{}, fmt.Errorf("bigint: cant convert decimal %q to *big.Int", decimal)
	}
	return Int{i}, nil
}

func FromHex(raw string) (Int, error) {
	i, ok := new(big.Int).SetString(strings.TrimPrefix(raw, "0x"), 16)
	if !ok {
		return Int{}, fmt.Errorf("bigint: can't convert hex %q to *big.Int", raw)
	}
	return Int{Int: i}, nil
}

func MustDecimal(raw string) Int {
	i, err := FromDecimal(raw)
	if err != nil {
		panic(err)
	}
	return i
}

func MustHex(raw string) Int {
	i, err := FromHex(raw)
	if err != nil {
		panic(err)
	}
	return i
}

// FromReadable 将最大单位，按照精度算出最小单位的值，如果算出的结果仍有
func FromReadable(readable float64, precision uint8) (Int, error) {
	// readable * 10 ** precision
	mul := decimal.NewFromFloat(float64(10)).Pow(decimal.NewFromFloat(float64(precision)))
	amount := decimal.NewFromFloat(readable).Mul(mul)

	bigint := new(big.Int)
	if _, ok := bigint.SetString(amount.String(), 10); !ok {
		return Int{}, fmt.Errorf("float64 %f with precision %d can't convert to bigint", readable, precision)
	}
	return Int{Int: bigint}, nil
}

// FromReadableString 将最大单位，按照精度算出最小单位的值，如果算出的结果仍有
func FromReadableString(readable string, precision uint8) (Int, error) {
	// readable * 10 ** precision
	mul := decimal.NewFromFloat(float64(10)).Pow(decimal.NewFromFloat(float64(precision)))
	amount, err := decimal.NewFromString(readable)
	if err != nil {
		return Int{}, err
	}
	amount = amount.Mul(mul)

	bigint := new(big.Int)
	if _, ok := bigint.SetString(amount.String(), 10); !ok {
		return Int{}, fmt.Errorf("float64 %s with precision %d can't convert to bigint", readable, precision)
	}
	return Int{Int: bigint}, nil
}

// Readable2Decimal 将最大单位的string转化为Decimal
func Readable2Decimal(readable string) (decimal.Decimal, error) {
	return decimal.NewFromString(readable)
}

// IsValidReadable 是否合法的金额, 非负数
func IsValidReadable(readable string) bool {
	amount, err := decimal.NewFromString(readable)
	if err != nil {
		return false
	}

	return !amount.IsNegative()
}

// IsPositive 是否正数
func IsPositive(readable string) bool {
	amount, err := decimal.NewFromString(readable)
	if err != nil {
		return false
	}

	return amount.IsPositive()
}

func FromBytes(bytes []byte) Int {
	return Int{Int: new(big.Int).SetBytes(bytes)}
}

func FromBig(big *big.Int) Int {
	return Int{big}
}

// Scan 数据库 decimal 实现
// 这里会保证始终能获取一个不为 nil 的 *big.Int
func (i *Int) Scan(val interface{}) error {
	if val == nil {
		i.Int = new(big.Int)
		return nil
	}

	var data string
	switch i := val.(type) {
	case []byte:
		data = string(i)
	case string:
		data = i
	default:
		return errors.New("Not supports type")
	}

	var ok bool
	i.Int, ok = new(big.Int).SetString(data, 10)
	if !ok {
		return ErrInvalidNumber
	}
	return nil
}

func (i Int) Value() (driver.Value, error) {
	return i.String(), nil
}

func (i Int) String() string {
	if i.Int == nil {
		return "0"
	}
	return i.Int.String()
}

// Big 返回底层 *big.Int 数据
func (i Int) Big() *big.Int {
	if i.Int == nil {
		return new(big.Int)
	}
	return i.Int
}

func (i Int) MarshalJSON() ([]byte, error) {
	if i.Int == nil {
		return []byte("null"), nil
	}
	return json.Marshal(i.Int.String())
}

var quote = []byte(`"`)
var null = []byte(`null`)
var hprx = []byte(`0x`) // `0x`

func (i *Int) UnmarshalJSON(text []byte) error {
	var ok bool
	if bytes.HasPrefix(text, quote) {
		n := text[1 : len(text)-1]
		if bytes.HasPrefix(n, hprx) {
			r := string(n[2:])
			if i.Int, ok = new(big.Int).SetString(r, 16); !ok {
				return fmt.Errorf(`bigint: can't convert "0x%s" to *big.Int`, r)
			}
			return nil
		}

		r := string(n)
		if i.Int, ok = new(big.Int).SetString(r, 10); !ok {
			return fmt.Errorf(`bigint: can't convert "%s" to *big.Int`, r)
		}
		return nil
	}

	if bytes.Equal(text, null) {
		i.Int = new(big.Int)
		return nil
	}

	r := string(text)
	if i.Int, ok = new(big.Int).SetString(r, 10); !ok {
		return fmt.Errorf("bigint: can't convert %s to *big.Int", r)
	}
	return nil
}

func (i Int) Readable(precision uint8) float64 {
	if i.Int == nil {
		return 0
	}
	num := decimal.NewFromBigInt(i.Int, 0)
	mul := decimal.NewFromInt(10).Pow(decimal.NewFromInt(int64(precision)))
	result, _ := num.Div(mul).Float64()
	return result
}

func (i Int) ReadableString(precision uint8) string {
	if i.Int == nil {
		return "0"
	}
	num := decimal.NewFromBigInt(i.Int, 0)
	mul, ok := exp.Load(precision)
	if !ok {
		mul = decimal.NewFromInt(10).Pow(decimal.NewFromInt(int64(precision)))
		exp.Store(precision, mul)
	}
	result := num.Div(mul.(decimal.Decimal)).String()
	return result
}

func (i Int) ReadableDecimal(precision uint8) decimal.Decimal {
	if i.Int == nil {
		return decimal.Decimal{}
	}
	num := decimal.NewFromBigInt(i.Int, 0)
	mul := decimal.NewFromInt(10).Pow(decimal.NewFromInt(int64(precision)))
	result := num.Div(mul)
	return result
}

func (i Int) Zero() bool {
	return i.Int == nil || i.BitLen() == 0
}

func (i Int) Positive() bool {
	return i.Sign() > 0
}

func (i Int) Copy() Int {
	return Int{new(big.Int).Set(i.Int)}
}

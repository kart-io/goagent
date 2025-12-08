// Package types 数据结构定义
//
// 类型定义独立于业务逻辑，便于复用和测试
package types

import "time"

// ============================================================================
// 请求结构
// ============================================================================

// WeatherRequest 天气查询请求
type WeatherRequest struct {
	City string `json:"city" validate:"required,min=1,max=50"`
	Days int    `json:"days,omitempty" validate:"omitempty,min=1,max=7"`
}

// ForecastRequest 天气预报请求
type ForecastRequest struct {
	City string `json:"city" validate:"required"`
	Days int    `json:"days" validate:"required,min=1,max=7"`
}

// ============================================================================
// 响应结构
// ============================================================================

// WeatherResponse 天气查询响应
type WeatherResponse struct {
	City        string      `json:"city"`
	Weather     WeatherInfo `json:"weather"`
	UpdatedAt   time.Time   `json:"updated_at"`
	CacheStatus string      `json:"cache_status,omitempty"`
}

// WeatherInfo 天气信息
type WeatherInfo struct {
	Condition   string  `json:"condition"`   // 天气状况：晴、多云、雨等
	Temperature float64 `json:"temperature"` // 当前温度
	HighTemp    float64 `json:"high_temp"`   // 最高温度
	LowTemp     float64 `json:"low_temp"`    // 最低温度
	Humidity    int     `json:"humidity"`    // 湿度百分比
	WindSpeed   float64 `json:"wind_speed"`  // 风速 km/h
	WindDir     string  `json:"wind_dir"`    // 风向
	UV          int     `json:"uv"`          // 紫外线指数
	AQI         int     `json:"aqi"`         // 空气质量指数
}

// ForecastResponse 天气预报响应
type ForecastResponse struct {
	City     string         `json:"city"`
	Forecast []ForecastItem `json:"forecast"`
}

// ForecastItem 预报项目
type ForecastItem struct {
	Date      string  `json:"date"`
	Condition string  `json:"condition"`
	HighTemp  float64 `json:"high_temp"`
	LowTemp   float64 `json:"low_temp"`
	Humidity  int     `json:"humidity"`
	WindSpeed float64 `json:"wind_speed"`
	WindDir   string  `json:"wind_dir"`
}

// ============================================================================
// Skill 相关类型
// ============================================================================

// SkillInput 技能输入
type SkillInput struct {
	Action string                 `json:"action"`
	Args   map[string]interface{} `json:"args"`
}

// SkillOutput 技能输出
type SkillOutput struct {
	Success    bool        `json:"success"`
	Result     interface{} `json:"result"`
	Error      string      `json:"error,omitempty"`
	Duration   string      `json:"duration"`
	SkillName  string      `json:"skill_name"`
	Action     string      `json:"action"`
	Confidence float64     `json:"confidence"`
}

// ============================================================================
// 错误类型
// ============================================================================

// Error 自定义错误类型
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	return e.Message
}

// 预定义错误
var (
	ErrCityNotSupported = &Error{Code: 1001, Message: "城市不在支持列表中"}
	ErrInvalidDays      = &Error{Code: 1002, Message: "预报天数无效，支持 1-7 天"}
	ErrWeatherNotFound  = &Error{Code: 1003, Message: "无法获取天气信息"}
	ErrInvalidAction    = &Error{Code: 1004, Message: "无效的操作类型"}
)

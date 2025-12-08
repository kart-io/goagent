// Package logic 业务逻辑层
//
// 所有业务逻辑集中在 logic 层
// Handler 层只负责请求解析和响应，不包含业务逻辑
package logic

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/svc"
	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/types"
)

// WeatherLogic 天气业务逻辑
type WeatherLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewWeatherLogic 创建天气业务逻辑
func NewWeatherLogic(ctx context.Context, svcCtx *svc.ServiceContext) *WeatherLogic {
	return &WeatherLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// GetWeather 获取当前天气
func (l *WeatherLogic) GetWeather(req *types.WeatherRequest) (*types.WeatherResponse, error) {
	// 验证城市是否支持
	if !l.isCitySupported(req.City) {
		return nil, types.ErrCityNotSupported
	}

	// 使用默认城市
	city := req.City
	if city == "" {
		city = l.svcCtx.Config.Skill.DefaultCity
	}

	// 检查缓存
	cacheKey := fmt.Sprintf("weather:%s", city)
	cacheStatus := "miss"

	if l.svcCtx.Cache != nil {
		if cached, ok := l.svcCtx.Cache.Get(cacheKey); ok {
			if resp, ok := cached.(*types.WeatherResponse); ok {
				resp.CacheStatus = "hit"
				return resp, nil
			}
		}
		cacheStatus = "miss"
	}

	// 模拟获取天气数据（实际应用中应调用真实 API）
	weather := l.generateMockWeather(city)

	resp := &types.WeatherResponse{
		City:        city,
		Weather:     weather,
		UpdatedAt:   time.Now(),
		CacheStatus: cacheStatus,
	}

	// 写入缓存
	if l.svcCtx.Cache != nil {
		l.svcCtx.Cache.Set(cacheKey, resp)
	}

	return resp, nil
}

// GetForecast 获取天气预报
func (l *WeatherLogic) GetForecast(req *types.ForecastRequest) (*types.ForecastResponse, error) {
	// 验证城市是否支持
	if !l.isCitySupported(req.City) {
		return nil, types.ErrCityNotSupported
	}

	// 验证天数
	if req.Days < 1 || req.Days > 7 {
		return nil, types.ErrInvalidDays
	}

	// 模拟生成预报数据
	forecast := make([]types.ForecastItem, req.Days)
	baseDate := time.Now()

	for i := 0; i < req.Days; i++ {
		date := baseDate.AddDate(0, 0, i)
		forecast[i] = l.generateMockForecastItem(date)
	}

	return &types.ForecastResponse{
		City:     req.City,
		Forecast: forecast,
	}, nil
}

// isCitySupported 检查城市是否支持
func (l *WeatherLogic) isCitySupported(city string) bool {
	for _, c := range l.svcCtx.Config.Skill.SupportedCities {
		if c == city {
			return true
		}
	}
	return false
}

// generateMockWeather 生成模拟天气数据
func (l *WeatherLogic) generateMockWeather(city string) types.WeatherInfo {
	conditions := []string{"晴", "多云", "阴", "小雨", "中雨", "雷阵雨", "雪"}
	windDirs := []string{"东风", "南风", "西风", "北风", "东南风", "西南风", "东北风", "西北风"}

	// 基于城市生成不同的基础温度
	baseTemp := l.getCityBaseTemp(city)

	return types.WeatherInfo{
		Condition:   conditions[rand.Intn(len(conditions))],
		Temperature: baseTemp + float64(rand.Intn(10)-5),
		HighTemp:    baseTemp + float64(rand.Intn(5)+3),
		LowTemp:     baseTemp - float64(rand.Intn(5)+3),
		Humidity:    rand.Intn(60) + 30,
		WindSpeed:   float64(rand.Intn(30) + 5),
		WindDir:     windDirs[rand.Intn(len(windDirs))],
		UV:          rand.Intn(11),
		AQI:         rand.Intn(200) + 20,
	}
}

// generateMockForecastItem 生成模拟预报数据
func (l *WeatherLogic) generateMockForecastItem(date time.Time) types.ForecastItem {
	conditions := []string{"晴", "多云", "阴", "小雨", "中雨"}
	windDirs := []string{"东风", "南风", "西风", "北风"}

	baseTemp := 20.0 + float64(rand.Intn(10)-5)

	return types.ForecastItem{
		Date:      date.Format("2006-01-02"),
		Condition: conditions[rand.Intn(len(conditions))],
		HighTemp:  baseTemp + float64(rand.Intn(8)+2),
		LowTemp:   baseTemp - float64(rand.Intn(8)+2),
		Humidity:  rand.Intn(50) + 40,
		WindSpeed: float64(rand.Intn(20) + 5),
		WindDir:   windDirs[rand.Intn(len(windDirs))],
	}
}

// getCityBaseTemp 获取城市基础温度
func (l *WeatherLogic) getCityBaseTemp(city string) float64 {
	// 简单的城市温度模拟
	temps := map[string]float64{
		"北京": 15.0,
		"上海": 20.0,
		"广州": 25.0,
		"深圳": 26.0,
		"杭州": 18.0,
		"成都": 17.0,
		"武汉": 19.0,
		"西安": 14.0,
	}

	if temp, ok := temps[city]; ok {
		return temp
	}
	return 20.0
}

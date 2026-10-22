package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	loggercore "github.com/kart-io/logger/core"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/multiagent"
)

// SentimentAnalysisResult 情感分析结果
type SentimentAnalysisResult struct {
	Platform          string  `json:"platform"`
	Polarity          float64 `json:"polarity"`     // 情感极性 (-1 到 1)
	SubjectivityScore float64 `json:"subjectivity"` // 主观性得分 (0 到 1)
	PostCount         int     `json:"post_count"`
	ProcessingTime    float64 `json:"processing_time"` // 处理时间（秒）
}

// AggregatedSentiment 聚合的情感分析报告
type AggregatedSentiment struct {
	WeightedPolarity float64                   `json:"weighted_polarity"`
	TotalPosts       int                       `json:"total_posts"`
	PlatformResults  []SentimentAnalysisResult `json:"platform_results"`
	ProcessingTime   float64                   `json:"processing_time"`
	Summary          string                    `json:"summary"`
}

// RunAggregatorPattern 运行聚合模式示例
func RunAggregatorPattern(llmClient llm.Client, logger loggercore.Logger) error {
	fmt.Println("\n🔄 模式 1: Aggregator（聚合模式）- 社交媒体情感分析")
	fmt.Println("说明：多个Agent并行分析不同平台的内容，聚合者综合所有结果生成最终报告")
	fmt.Println(strings.Repeat("=", 79))

	// 创建多智能体系统
	system := multiagent.NewMultiAgentSystem(logger)

	// 创建平台分析 Agent
	twitterAgent := createSentimentAgent(llmClient, "twitter", "Twitter")
	instagramAgent := createSentimentAgent(llmClient, "instagram", "Instagram")
	redditAgent := createSentimentAgent(llmClient, "reddit", "Reddit")

	// 注册 Agent
	if err := system.RegisterAgent("twitter", twitterAgent); err != nil {
		return fmt.Errorf("注册 Twitter Agent 失败: %w", err)
	}
	if err := system.RegisterAgent("instagram", instagramAgent); err != nil {
		return fmt.Errorf("注册 Instagram Agent 失败: %w", err)
	}
	if err := system.RegisterAgent("reddit", redditAgent); err != nil {
		return fmt.Errorf("注册 Reddit Agent 失败: %w", err)
	}

	// 创建聚合任务
	task := &multiagent.CollaborativeTask{
		ID:          "sentiment_analysis_001",
		Name:        "社交媒体情感分析",
		Description: "分析多个社交媒体平台关于某产品的情感倾向",
		Type:        multiagent.CollaborationTypeParallel,
		Input:       "分析关于'新款智能手机XYZ'的社交媒体情感",
		Assignments: make(map[string]multiagent.Assignment),
	}

	fmt.Printf("\n📊 任务: %s\n", task.Description)
	fmt.Printf("🎯 输入: %s\n\n", task.Input)

	startTime := time.Now()

	// 执行并行任务
	ctx := context.Background()
	result, err := system.ExecuteTask(ctx, task)
	if err != nil {
		return fmt.Errorf("执行任务失败: %w", err)
	}

	duration := time.Since(startTime)

	// 聚合结果
	aggregated := aggregateSentimentResults(result.Results, duration.Seconds())

	// 输出结果
	fmt.Println("✅ 分析完成！")
	fmt.Println("\n📈 各平台分析结果:")
	fmt.Println(strings.Repeat("-", 79))
	for _, platformResult := range aggregated.PlatformResults {
		fmt.Printf("\n%s:\n", platformResult.Platform)
		fmt.Printf("  情感极性: %.2f (-1=负面, 0=中性, 1=正面)\n", platformResult.Polarity)
		fmt.Printf("  主观性得分: %.2f\n", platformResult.SubjectivityScore)
		fmt.Printf("  帖子数量: %d\n", platformResult.PostCount)
		fmt.Printf("  处理时间: %.2f秒\n", platformResult.ProcessingTime)
	}

	fmt.Println("\n📊 聚合报告:")
	fmt.Println(strings.Repeat("-", 79))
	fmt.Printf("加权情感极性: %.2f\n", aggregated.WeightedPolarity)
	fmt.Printf("总帖子数: %d\n", aggregated.TotalPosts)
	fmt.Printf("总处理时间: %.2f秒\n", aggregated.ProcessingTime)
	fmt.Printf("\n摘要: %s\n", aggregated.Summary)
	fmt.Println(strings.Repeat("-", 79))

	fmt.Printf("\n⏱️  执行时间: %v\n", duration)
	fmt.Printf("\n💡 性能优势: 并行处理比顺序处理节省了约 %.1f%%的时间\n",
		((float64(len(aggregated.PlatformResults))*2.0)-duration.Seconds())/
			(float64(len(aggregated.PlatformResults))*2.0)*100)

	return nil
}

// createSentimentAgent 创建情感分析 Agent
func createSentimentAgent(llmClient llm.Client, agentID, platform string) multiagent.CollaborativeAgent {
	agent := NewCollaborativeMockAgent(agentID, fmt.Sprintf("%s情感分析专家", platform), multiagent.RoleSpecialist)

	agent.SetInvokeFn(func(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
		startTime := time.Now()

		// 模拟分析（实际应用中会调用 LLM）
		// 这里简化处理，实际应该调用 LLM
		// response, err := llmClient.Complete(ctx, &llm.CompletionRequest{...})

		// 模拟结果
		var polarity, subjectivity float64
		var postCount int
		switch platform {
		case "Twitter":
			polarity = 0.6
			subjectivity = 0.7
			postCount = 1500
		case "Instagram":
			polarity = 0.8
			subjectivity = 0.6
			postCount = 800
		case "Reddit":
			polarity = 0.3
			subjectivity = 0.8
			postCount = 600
		}

		result := SentimentAnalysisResult{
			Platform:          platform,
			Polarity:          polarity,
			SubjectivityScore: subjectivity,
			PostCount:         postCount,
			ProcessingTime:    time.Since(startTime).Seconds(),
		}

		return &core.AgentOutput{
			Result: result,
			Status: "success",
		}, nil
	})

	return agent
}

// aggregateSentimentResults 聚合情感分析结果
func aggregateSentimentResults(results map[string]interface{}, processingTime float64) *AggregatedSentiment {
	var platformResults []SentimentAnalysisResult
	var totalPosts int
	var weightedSum float64

	for _, result := range results {
		if sentimentResult, ok := result.(SentimentAnalysisResult); ok {
			platformResults = append(platformResults, sentimentResult)
			totalPosts += sentimentResult.PostCount
			weightedSum += sentimentResult.Polarity * float64(sentimentResult.PostCount)
		}
	}

	weightedPolarity := weightedSum / float64(totalPosts)

	// 生成摘要
	var sentiment string
	if weightedPolarity > 0.5 {
		sentiment = "整体情感非常正面"
	} else if weightedPolarity > 0.2 {
		sentiment = "整体情感偏向正面"
	} else if weightedPolarity > -0.2 {
		sentiment = "整体情感中性"
	} else if weightedPolarity > -0.5 {
		sentiment = "整体情感偏向负面"
	} else {
		sentiment = "整体情感非常负面"
	}

	summary := fmt.Sprintf("%s。跨%d个平台分析了%d条帖子，加权情感极性为%.2f。",
		sentiment, len(platformResults), totalPosts, weightedPolarity)

	return &AggregatedSentiment{
		WeightedPolarity: weightedPolarity,
		TotalPosts:       totalPosts,
		PlatformResults:  platformResults,
		ProcessingTime:   processingTime,
		Summary:          summary,
	}
}

// CollectPlatformData 模拟收集平台数据（并行）
func CollectPlatformData(platforms []string) map[string][]string {
	var wg sync.WaitGroup
	results := make(map[string][]string)
	var mu sync.Mutex

	for _, platform := range platforms {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()

			// 模拟数据收集延迟
			time.Sleep(100 * time.Millisecond)

			// 模拟收集到的帖子
			posts := []string{
				fmt.Sprintf("%s帖子1: 这个产品很棒！", p),
				fmt.Sprintf("%s帖子2: 还不错，值得购买", p),
				fmt.Sprintf("%s帖子3: 有些失望...", p),
			}

			mu.Lock()
			results[p] = posts
			mu.Unlock()
		}(platform)
	}

	wg.Wait()
	return results
}

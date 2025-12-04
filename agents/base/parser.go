package base

import (
	"regexp"
	"strconv"
	"strings"
)

// LanguageKeywords 定义特定语言的关键词集合
type LanguageKeywords struct {
	// 步骤相关
	StepPrefixes     []string // 步骤前缀，如 "Step", "步骤"
	StepSeparators   []string // 步骤分隔符，如 ". ", "、"
	SequenceWords    []string // 顺序词，如 "首先", "其次", "然后"
	ListMarkers      []string // 列表标记，如 "- ", "* "
	NumberedPrefixes []string // 数字前缀，如 "第一", "第二"

	// 答案相关
	AnswerMarkers     []string // 答案标记，如 "Answer:", "答案："
	ConclusionMarkers []string // 结论标记，如 "Therefore", "因此"

	// 问题相关
	QuestionPrefixes   []string // 问题前缀，如 "Q:", "问题："
	QuestionWords      []string // 疑问词，如 "what", "什么"
	DirectAnswerSignal []string // 直接回答信号，如 "DIRECT_ANSWER", "直接回答"

	// 复杂度指标
	ComplexityIndicators []string // 复杂度指标，如 "and", "或者"

	// 信息需求指标
	InfoIndicators []string // 信息需求指标，如 "what is", "是什么"

	// 置信度相关
	UncertaintyMarkers []string // 不确定标记，如 "maybe", "可能"
	ConfidenceMarkers  []string // 确定标记，如 "definitely", "肯定"

	// 改进指标
	RefinementIndicators []string // 改进指标，如 "incorrect", "错误"

	// 思考关联
	RelationKeywords []string // 关联关键词，如 "therefore", "因此"

	// 推理词汇（用于评估思考质量）
	ReasoningWords []string // 推理词汇，如 "because", "therefore", "因为", "所以"
}

// ParserConfig 解析器配置
type ParserConfig struct {
	// 语言关键词映射，key 为语言代码（如 "en", "zh"）
	Languages map[string]*LanguageKeywords

	// 是否启用特定语言
	EnabledLanguages []string

	// 自定义分隔符模式
	CustomSeparators []string

	// 数字检测正则
	NumberPattern *regexp.Regexp
}

// ResponseParser 响应解析器接口
type ResponseParser interface {
	// ParseSteps 从响应中解析步骤
	ParseSteps(response string) []string

	// ParseAnswer 从响应中解析答案
	ParseAnswer(response string) string

	// ParseQuestions 从响应中解析问题列表
	ParseQuestions(response string, parentID string) []ParsedQuestion

	// IsStepLine 判断是否是步骤行
	IsStepLine(line string) (bool, string)

	// IsAnswerLine 判断是否是答案行
	IsAnswerLine(line string) bool

	// ExtractStepContent 从步骤行提取内容
	ExtractStepContent(line string) string

	// ExtractAnswerContent 从答案行提取内容
	ExtractAnswerContent(line string) string

	// ContainsQuestionWords 检查是否包含疑问词
	ContainsQuestionWords(text string) bool

	// ShouldDecompose 判断是否应该分解问题
	ShouldDecompose(question string) bool

	// NeedsExternalInfo 判断是否需要外部信息
	NeedsExternalInfo(question string) bool

	// EstimateConfidence 估算答案置信度
	EstimateConfidence(answer string) float64

	// NeedsRefinement 判断是否需要改进
	NeedsRefinement(critique string) bool

	// AreThoughtsRelated 判断两个思考是否相关
	AreThoughtsRelated(thought1, thought2 string) bool
}

// ParsedQuestion 解析出的问题
type ParsedQuestion struct {
	ID       string
	Text     string
	Type     string // "followup", "decomposed", etc.
	ParentID string
}

// DefaultParser 默认多语言解析器实现
type DefaultParser struct {
	config *ParserConfig
}

// NewDefaultParser 创建默认解析器
func NewDefaultParser() *DefaultParser {
	return &DefaultParser{
		config: DefaultParserConfig(),
	}
}

// NewParserWithConfig 使用自定义配置创建解析器
func NewParserWithConfig(config *ParserConfig) *DefaultParser {
	return &DefaultParser{
		config: config,
	}
}

// DefaultParserConfig 返回默认解析器配置（中英文支持）
func DefaultParserConfig() *ParserConfig {
	return &ParserConfig{
		EnabledLanguages: []string{"en", "zh"},
		Languages: map[string]*LanguageKeywords{
			"en": DefaultEnglishKeywords(),
			"zh": DefaultChineseKeywords(),
		},
		CustomSeparators: []string{},
		NumberPattern:    regexp.MustCompile(`^\d+$`),
	}
}

// DefaultEnglishKeywords 返回默认英文关键词
func DefaultEnglishKeywords() *LanguageKeywords {
	return &LanguageKeywords{
		// 步骤相关
		StepPrefixes:     []string{"step", "stage", "phase"},
		StepSeparators:   []string{". ", ": ", ") "},
		SequenceWords:    []string{"first", "second", "third", "then", "next", "finally", "lastly"},
		ListMarkers:      []string{"- ", "* ", "• "},
		NumberedPrefixes: []string{},

		// 答案相关
		AnswerMarkers: []string{
			"the final answer is",
			"final answer:",
			"answer:",
			"result:",
		},
		ConclusionMarkers: []string{
			"therefore,",
			"thus,",
			"so,",
			"hence,",
			"in conclusion,",
			"to conclude,",
			"consequently,",
		},

		// 问题相关
		QuestionPrefixes:   []string{"q:", "question:", "q1:", "q2:", "q3:", "question 1:", "question 2:", "question 3:"},
		QuestionWords:      []string{"what", "why", "how", "when", "where", "which", "who", "whose", "whom"},
		DirectAnswerSignal: []string{"DIRECT_ANSWER", "direct answer", "can be answered directly"},

		// 复杂度指标
		ComplexityIndicators: []string{
			"and", "or", "multiple", "several", "various",
			"compare", "contrast", "analyze", "evaluate",
			"both", "either", "neither", "all", "each",
		},

		// 信息需求指标
		InfoIndicators: []string{
			"what is", "who is", "when did", "where is",
			"how many", "which", "define", "explain",
			"describe", "list", "name",
		},

		// 置信度相关
		UncertaintyMarkers: []string{
			"maybe", "possibly", "might", "could be", "not sure",
			"uncertain", "perhaps", "probably", "likely",
			"it seems", "appears to be", "i think",
		},
		ConfidenceMarkers: []string{
			"definitely", "certainly", "clearly", "obviously",
			"absolutely", "undoubtedly", "surely", "indeed",
			"without doubt", "for certain",
		},

		// 改进指标
		RefinementIndicators: []string{
			"incorrect", "wrong", "missing", "incomplete",
			"should", "needs", "must", "improve",
			"error", "mistake", "flaw", "issue",
			"could be better", "needs improvement",
		},

		// 思考关联
		RelationKeywords: []string{
			"therefore", "because", "result", "conclusion",
			"analysis", "solution", "approach", "method",
			"consequently", "hence", "thus",
		},

		// 推理词汇（用于评估思考质量）
		ReasoningWords: []string{
			"because", "therefore", "thus", "since", "if", "then",
			"consequently", "hence", "so", "given", "assuming",
			"implies", "conclude", "reason", "analyze", "consider",
		},
	}
}

// DefaultChineseKeywords 返回默认中文关键词
func DefaultChineseKeywords() *LanguageKeywords {
	return &LanguageKeywords{
		// 步骤相关
		StepPrefixes:     []string{"步骤", "第", "阶段"},
		StepSeparators:   []string{"、", "。", "：", ":", "）", ")"},
		SequenceWords:    []string{"首先", "其次", "然后", "接下来", "最后", "第一", "第二", "第三"},
		ListMarkers:      []string{"- ", "* ", "· ", "• "},
		NumberedPrefixes: []string{"第一", "第二", "第三", "第四", "第五", "第六", "第七", "第八", "第九", "第十"},

		// 答案相关
		AnswerMarkers: []string{
			"最终答案是",
			"最终答案：",
			"最终答案:",
			"答案是",
			"答案：",
			"答案:",
			"结果是",
			"结果：",
		},
		ConclusionMarkers: []string{
			"因此，",
			"因此,",
			"所以，",
			"所以,",
			"综上所述，",
			"综上所述,",
			"总结：",
			"总结:",
			"结论：",
			"结论:",
			"由此可见，",
		},

		// 问题相关
		QuestionPrefixes: []string{
			"问题：", "问题:", "问：", "问:",
			"子问题：", "子问题:",
			"问题1：", "问题1:", "问题2：", "问题2:", "问题3：", "问题3:",
			"问题一：", "问题一:", "问题二：", "问题二:", "问题三：", "问题三:",
		},
		QuestionWords:      []string{"什么", "为什么", "怎么", "如何", "何时", "哪里", "哪个", "谁", "是否", "能否", "可否", "是不是", "有没有"},
		DirectAnswerSignal: []string{"直接回答", "可以直接回答", "不需要分解", "无需分解"},

		// 复杂度指标
		ComplexityIndicators: []string{
			"和", "或者", "以及", "同时", "多个", "几个", "各种",
			"比较", "对比", "分析", "评估", "综合", "多方面",
			"首先", "其次", "另外", "此外", "不仅", "而且",
			"既要", "又要", "一方面", "另一方面",
		},

		// 信息需求指标
		InfoIndicators: []string{
			"什么是", "谁是", "何时", "哪里", "在哪",
			"多少", "哪个", "哪些", "定义", "解释",
			"是什么", "有什么", "为什么", "怎么样", "如何",
			"描述", "列举", "说明",
		},

		// 置信度相关
		UncertaintyMarkers: []string{
			"可能", "也许", "或许", "大概", "不确定", "不太确定",
			"似乎", "好像", "应该是", "估计", "猜测",
			"不一定", "未必", "说不定",
		},
		ConfidenceMarkers: []string{
			"肯定", "一定", "确定", "明确", "显然", "毫无疑问",
			"必然", "绝对", "当然", "无疑", "确实",
			"毋庸置疑", "不容置疑",
		},

		// 改进指标
		RefinementIndicators: []string{
			"错误", "不正确", "缺失", "不完整", "遗漏",
			"应该", "需要", "必须", "改进", "改善",
			"问题", "缺陷", "不足", "欠缺", "有待",
			"建议", "可以改进", "需要补充", "待完善",
		},

		// 思考关联
		RelationKeywords: []string{
			"因此", "所以", "结果", "结论", "分析", "解决",
			"方案", "方法", "原因", "基于", "根据",
			"导致", "引起", "造成", "由于",
		},

		// 推理词汇（用于评估思考质量）
		ReasoningWords: []string{
			"因为", "所以", "因此", "如果", "那么", "首先", "其次", "最后",
			"分析", "考虑", "推断", "推理", "判断", "结论", "由此可见",
			"综上所述", "总之", "假设", "假如", "既然", "于是",
		},
	}
}

// 实现 ResponseParser 接口

// ParseSteps 从响应中解析步骤
func (p *DefaultParser) ParseSteps(response string) []string {
	lines := strings.Split(response, "\n")
	steps := make([]string, 0)
	currentStep := strings.Builder{}
	inStep := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// 检查是否是答案行，如果是则结束
		if p.IsAnswerLine(line) {
			if currentStep.Len() > 0 {
				steps = append(steps, strings.TrimSpace(currentStep.String()))
			}
			break
		}

		// 检查是否是步骤行
		isStep, content := p.IsStepLine(line)
		if isStep {
			// 保存前一个步骤
			if currentStep.Len() > 0 {
				steps = append(steps, strings.TrimSpace(currentStep.String()))
				currentStep.Reset()
			}
			inStep = true
			if content != "" {
				currentStep.WriteString(content)
				currentStep.WriteString(" ")
			}
		} else if inStep {
			// 跳过特殊行
			if p.shouldSkipLine(line) {
				continue
			}
			currentStep.WriteString(line)
			currentStep.WriteString(" ")
		}
	}

	// 保存最后一个步骤
	if currentStep.Len() > 0 {
		steps = append(steps, strings.TrimSpace(currentStep.String()))
	}

	return steps
}

// ParseAnswer 从响应中解析答案
func (p *DefaultParser) ParseAnswer(response string) string {
	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if p.IsAnswerLine(line) {
			return p.ExtractAnswerContent(line)
		}
	}
	return ""
}

// ParseQuestions 从响应中解析问题列表
func (p *DefaultParser) ParseQuestions(response string, parentID string) []ParsedQuestion {
	// 检查是否是直接回答信号
	for _, lang := range p.config.EnabledLanguages {
		if keywords, ok := p.config.Languages[lang]; ok {
			for _, signal := range keywords.DirectAnswerSignal {
				if strings.Contains(response, signal) {
					return nil
				}
			}
		}
	}

	questions := make([]ParsedQuestion, 0)
	lines := strings.Split(response, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		text := p.extractQuestionText(line)
		if text != "" {
			questions = append(questions, ParsedQuestion{
				ID:       generateQuestionID(parentID, len(questions)+1),
				Text:     text,
				Type:     "followup",
				ParentID: parentID,
			})
		}

		// 限制问题数量
		if len(questions) >= 5 {
			break
		}
	}

	return questions
}

// IsStepLine 判断是否是步骤行
func (p *DefaultParser) IsStepLine(line string) (bool, string) {
	lowerLine := strings.ToLower(line)

	// 检查所有启用的语言
	for _, lang := range p.config.EnabledLanguages {
		keywords, ok := p.config.Languages[lang]
		if !ok {
			continue
		}

		// 检查步骤前缀
		for _, prefix := range keywords.StepPrefixes {
			if strings.HasPrefix(lowerLine, prefix) || strings.HasPrefix(line, prefix) {
				content := p.extractAfterPrefix(line, prefix)
				return true, content
			}
		}

		// 检查顺序词
		for _, word := range keywords.SequenceWords {
			if strings.HasPrefix(lowerLine, word) || strings.HasPrefix(line, word) {
				content := p.extractAfterPrefix(line, word)
				return true, content
			}
		}

		// 检查数字前缀（中文）
		for _, prefix := range keywords.NumberedPrefixes {
			if strings.HasPrefix(line, prefix) {
				content := p.extractAfterChineseNumber(line, prefix)
				return true, content
			}
		}
	}

	// 检查数字开头的列表格式
	if len(line) > 2 {
		firstChar := line[0]
		if firstChar >= '0' && firstChar <= '9' {
			rest := line[1:]
			for _, lang := range p.config.EnabledLanguages {
				if keywords, ok := p.config.Languages[lang]; ok {
					for _, sep := range keywords.StepSeparators {
						if strings.HasPrefix(rest, sep) {
							return true, strings.TrimSpace(rest[len(sep):])
						}
					}
				}
			}
			// 处理两位数字
			if len(line) > 3 && line[1] >= '0' && line[1] <= '9' {
				rest2 := line[2:]
				for _, lang := range p.config.EnabledLanguages {
					if keywords, ok := p.config.Languages[lang]; ok {
						for _, sep := range keywords.StepSeparators {
							if strings.HasPrefix(rest2, sep) {
								return true, strings.TrimSpace(rest2[len(sep):])
							}
						}
					}
				}
			}
		}
	}

	// 检查列表标记
	for _, lang := range p.config.EnabledLanguages {
		if keywords, ok := p.config.Languages[lang]; ok {
			for _, marker := range keywords.ListMarkers {
				if strings.HasPrefix(line, marker) {
					return true, strings.TrimSpace(line[len(marker):])
				}
			}
		}
	}

	// 检查 Markdown 格式
	cleanLine := strings.TrimPrefix(line, "**")
	cleanLine = strings.TrimSuffix(cleanLine, "**")
	if cleanLine != line {
		return p.IsStepLine(cleanLine)
	}

	return false, ""
}

// IsAnswerLine 判断是否是答案行
func (p *DefaultParser) IsAnswerLine(line string) bool {
	lowerLine := strings.ToLower(line)

	for _, lang := range p.config.EnabledLanguages {
		keywords, ok := p.config.Languages[lang]
		if !ok {
			continue
		}

		// 检查答案标记
		for _, marker := range keywords.AnswerMarkers {
			if strings.Contains(lowerLine, strings.ToLower(marker)) || strings.Contains(line, marker) {
				return true
			}
		}

		// 检查结论标记
		for _, marker := range keywords.ConclusionMarkers {
			if strings.Contains(lowerLine, strings.ToLower(marker)) || strings.Contains(line, marker) {
				return true
			}
		}
	}

	return false
}

// ExtractStepContent 从步骤行提取内容
func (p *DefaultParser) ExtractStepContent(line string) string {
	_, content := p.IsStepLine(line)
	return content
}

// ExtractAnswerContent 从答案行提取内容
func (p *DefaultParser) ExtractAnswerContent(line string) string {
	lowerLine := strings.ToLower(line)

	for _, lang := range p.config.EnabledLanguages {
		keywords, ok := p.config.Languages[lang]
		if !ok {
			continue
		}

		// 检查答案标记
		for _, marker := range keywords.AnswerMarkers {
			if idx := strings.Index(lowerLine, strings.ToLower(marker)); idx != -1 {
				return strings.TrimSpace(line[idx+len(marker):])
			}
			if idx := strings.Index(line, marker); idx != -1 {
				return strings.TrimSpace(line[idx+len(marker):])
			}
		}

		// 检查结论标记
		for _, marker := range keywords.ConclusionMarkers {
			if idx := strings.Index(lowerLine, strings.ToLower(marker)); idx != -1 {
				return strings.TrimSpace(line[idx+len(marker):])
			}
			if idx := strings.Index(line, marker); idx != -1 {
				return strings.TrimSpace(line[idx+len(marker):])
			}
		}
	}

	return strings.TrimSpace(line)
}

// ContainsQuestionWords 检查是否包含疑问词
func (p *DefaultParser) ContainsQuestionWords(text string) bool {
	lowerText := strings.ToLower(text)

	for _, lang := range p.config.EnabledLanguages {
		keywords, ok := p.config.Languages[lang]
		if !ok {
			continue
		}

		for _, word := range keywords.QuestionWords {
			if strings.Contains(lowerText, strings.ToLower(word)) || strings.Contains(text, word) {
				return true
			}
		}
	}

	return false
}

// ShouldDecompose 判断是否应该分解问题
func (p *DefaultParser) ShouldDecompose(question string) bool {
	lowerQuestion := strings.ToLower(question)
	score := 0

	for _, lang := range p.config.EnabledLanguages {
		keywords, ok := p.config.Languages[lang]
		if !ok {
			continue
		}

		for _, indicator := range keywords.ComplexityIndicators {
			if strings.Contains(lowerQuestion, strings.ToLower(indicator)) || strings.Contains(question, indicator) {
				score++
			}
		}
	}

	// 使用字符数判断（对中文更准确）
	runeCount := len([]rune(question))
	return score >= 2 || len(strings.Fields(question)) > 20 || runeCount > 50
}

// NeedsExternalInfo 判断是否需要外部信息
func (p *DefaultParser) NeedsExternalInfo(question string) bool {
	lowerQuestion := strings.ToLower(question)

	for _, lang := range p.config.EnabledLanguages {
		keywords, ok := p.config.Languages[lang]
		if !ok {
			continue
		}

		for _, indicator := range keywords.InfoIndicators {
			if strings.Contains(lowerQuestion, strings.ToLower(indicator)) || strings.Contains(question, indicator) {
				return true
			}
		}
	}

	return false
}

// EstimateConfidence 估算答案置信度
func (p *DefaultParser) EstimateConfidence(answer string) float64 {
	confidence := 0.5
	lowerAnswer := strings.ToLower(answer)

	// 详细答案增加置信度
	if len(answer) > 100 {
		confidence += 0.2
	}

	for _, lang := range p.config.EnabledLanguages {
		keywords, ok := p.config.Languages[lang]
		if !ok {
			continue
		}

		// 不确定性标记降低置信度
		for _, marker := range keywords.UncertaintyMarkers {
			if strings.Contains(lowerAnswer, strings.ToLower(marker)) || strings.Contains(answer, marker) {
				confidence -= 0.1
			}
		}

		// 确定性标记提高置信度
		for _, marker := range keywords.ConfidenceMarkers {
			if strings.Contains(lowerAnswer, strings.ToLower(marker)) || strings.Contains(answer, marker) {
				confidence += 0.1
			}
		}
	}

	// 限制在 [0, 1] 范围内
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}

	return confidence
}

// NeedsRefinement 判断是否需要改进
func (p *DefaultParser) NeedsRefinement(critique string) bool {
	lowerCritique := strings.ToLower(critique)

	for _, lang := range p.config.EnabledLanguages {
		keywords, ok := p.config.Languages[lang]
		if !ok {
			continue
		}

		for _, indicator := range keywords.RefinementIndicators {
			if strings.Contains(lowerCritique, strings.ToLower(indicator)) || strings.Contains(critique, indicator) {
				return true
			}
		}
	}

	return false
}

// AreThoughtsRelated 判断两个思考是否相关
func (p *DefaultParser) AreThoughtsRelated(thought1, thought2 string) bool {
	lower1 := strings.ToLower(thought1)
	lower2 := strings.ToLower(thought2)

	for _, lang := range p.config.EnabledLanguages {
		keywords, ok := p.config.Languages[lang]
		if !ok {
			continue
		}

		for _, keyword := range keywords.RelationKeywords {
			lowerKeyword := strings.ToLower(keyword)
			if (strings.Contains(lower1, lowerKeyword) || strings.Contains(thought1, keyword)) &&
				(strings.Contains(lower2, lowerKeyword) || strings.Contains(thought2, keyword)) {
				return true
			}
		}
	}

	return false
}

// ContainsReasoningWords 检查文本是否包含推理词汇
// 用于评估思考质量，支持多语言
func (p *DefaultParser) ContainsReasoningWords(text string) bool {
	lowerText := strings.ToLower(text)

	for _, lang := range p.config.EnabledLanguages {
		keywords, ok := p.config.Languages[lang]
		if !ok {
			continue
		}

		for _, word := range keywords.ReasoningWords {
			if strings.Contains(lowerText, strings.ToLower(word)) || strings.Contains(text, word) {
				return true
			}
		}
	}

	return false
}

// CountReasoningWords 统计文本中包含的推理词汇数量
// 返回匹配的推理词汇数量，用于更精细的评估
func (p *DefaultParser) CountReasoningWords(text string) int {
	lowerText := strings.ToLower(text)
	count := 0
	seen := make(map[string]bool) // 避免重复计数

	for _, lang := range p.config.EnabledLanguages {
		keywords, ok := p.config.Languages[lang]
		if !ok {
			continue
		}

		for _, word := range keywords.ReasoningWords {
			lowerWord := strings.ToLower(word)
			if seen[lowerWord] {
				continue
			}
			if strings.Contains(lowerText, lowerWord) || strings.Contains(text, word) {
				count++
				seen[lowerWord] = true
			}
		}
	}

	return count
}

// GetReasoningWords 获取所有启用语言的推理词汇
// 用于外部自定义或查看当前配置
func (p *DefaultParser) GetReasoningWords() []string {
	words := make([]string, 0)
	seen := make(map[string]bool)

	for _, lang := range p.config.EnabledLanguages {
		keywords, ok := p.config.Languages[lang]
		if !ok {
			continue
		}

		for _, word := range keywords.ReasoningWords {
			if !seen[word] {
				words = append(words, word)
				seen[word] = true
			}
		}
	}

	return words
}

// 辅助方法

func (p *DefaultParser) extractAfterPrefix(line, prefix string) string {
	// 找到前缀位置
	lowerLine := strings.ToLower(line)
	lowerPrefix := strings.ToLower(prefix)
	idx := strings.Index(lowerLine, lowerPrefix)
	if idx == -1 {
		idx = strings.Index(line, prefix)
	}
	if idx == -1 {
		return ""
	}

	rest := line[idx+len(prefix):]

	// 跳过可能的分隔符
	for _, lang := range p.config.EnabledLanguages {
		if keywords, ok := p.config.Languages[lang]; ok {
			for _, sep := range keywords.StepSeparators {
				if strings.HasPrefix(rest, sep) {
					return strings.TrimSpace(rest[len(sep):])
				}
			}
		}
	}

	// 跳过数字
	rest = strings.TrimSpace(rest)
	if len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
		// 跳过数字和后续分隔符
		i := 0
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
		rest = rest[i:]
		for _, lang := range p.config.EnabledLanguages {
			if keywords, ok := p.config.Languages[lang]; ok {
				for _, sep := range keywords.StepSeparators {
					if strings.HasPrefix(rest, sep) {
						return strings.TrimSpace(rest[len(sep):])
					}
				}
			}
		}
	}

	return strings.TrimSpace(rest)
}

func (p *DefaultParser) extractAfterChineseNumber(line, prefix string) string {
	rest := line[len(prefix):]
	// 跳过可能的 "步"、"点" 等后缀
	suffixes := []string{"步", "点", "阶段"}
	for _, suffix := range suffixes {
		rest = strings.TrimPrefix(rest, suffix)
	}
	// 查找分隔符
	for _, lang := range p.config.EnabledLanguages {
		if keywords, ok := p.config.Languages[lang]; ok {
			for _, sep := range keywords.StepSeparators {
				if strings.HasPrefix(rest, sep) {
					return strings.TrimSpace(rest[len(sep):])
				}
			}
		}
	}
	return strings.TrimSpace(rest)
}

func (p *DefaultParser) extractQuestionText(line string) string {
	// 检查问题前缀
	for _, lang := range p.config.EnabledLanguages {
		keywords, ok := p.config.Languages[lang]
		if !ok {
			continue
		}

		for _, prefix := range keywords.QuestionPrefixes {
			lowerLine := strings.ToLower(line)
			lowerPrefix := strings.ToLower(prefix)
			if strings.HasPrefix(lowerLine, lowerPrefix) {
				return strings.TrimSpace(line[len(prefix):])
			}
			if strings.HasPrefix(line, prefix) {
				return strings.TrimSpace(line[len(prefix):])
			}
		}
	}

	// 检查数字列表格式
	if len(line) > 2 {
		firstChar := line[0]
		if firstChar >= '1' && firstChar <= '9' {
			for _, lang := range p.config.EnabledLanguages {
				if keywords, ok := p.config.Languages[lang]; ok {
					for _, sep := range keywords.StepSeparators {
						if strings.HasPrefix(line[1:], sep) {
							content := strings.TrimSpace(line[1+len(sep):])
							// 检查是否像是问题
							if strings.HasSuffix(content, "?") || strings.HasSuffix(content, "？") ||
								p.ContainsQuestionWords(content) {
								return content
							}
						}
					}
				}
			}
		}
	}

	// 检查是否是问句（以问号结尾）
	if strings.HasSuffix(line, "?") || strings.HasSuffix(line, "？") {
		// 去除列表标记
		for _, lang := range p.config.EnabledLanguages {
			if keywords, ok := p.config.Languages[lang]; ok {
				for _, marker := range keywords.ListMarkers {
					line = strings.TrimPrefix(line, marker)
				}
			}
		}
		return strings.TrimSpace(line)
	}

	return ""
}

func (p *DefaultParser) shouldSkipLine(line string) bool {
	lowerLine := strings.ToLower(line)

	// 跳过空白和特殊标记
	if line == "" || line == "\\[" || line == "\\]" {
		return true
	}

	// 跳过纯数字行
	if p.config.NumberPattern.MatchString(line) {
		return true
	}

	// 跳过 LaTeX 公式
	if strings.HasPrefix(line, "\\frac") || strings.HasPrefix(line, "\\quad") ||
		strings.HasPrefix(line, "\\text") {
		return true
	}

	// 跳过提示性文本
	skipPrefixes := []string{
		"question:", "let's", "here are",
		"问题：", "问题:", "让我们", "以下是",
	}
	for _, prefix := range skipPrefixes {
		if strings.HasPrefix(lowerLine, prefix) || strings.HasPrefix(line, prefix) {
			return true
		}
	}

	return false
}

func generateQuestionID(parentID string, index int) string {
	return parentID + "_fq_" + strconv.Itoa(index)
}

// intToString 辅助函数：整数转字符串
func intToString(n int) string {
	return strconv.Itoa(n)
}

// Stoi converts string to int
func Stoi(s string) int {
	var n int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

// AddLanguage 添加新的语言支持
func (p *DefaultParser) AddLanguage(code string, keywords *LanguageKeywords) {
	p.config.Languages[code] = keywords
	// 检查是否已启用
	for _, lang := range p.config.EnabledLanguages {
		if lang == code {
			return
		}
	}
	p.config.EnabledLanguages = append(p.config.EnabledLanguages, code)
}

// SetEnabledLanguages 设置启用的语言列表
func (p *DefaultParser) SetEnabledLanguages(languages []string) {
	p.config.EnabledLanguages = languages
}

// GetConfig 获取当前配置（用于自定义）
func (p *DefaultParser) GetConfig() *ParserConfig {
	return p.config
}

// Clone 克隆解析器（用于创建自定义版本）
func (p *DefaultParser) Clone() *DefaultParser {
	newConfig := &ParserConfig{
		EnabledLanguages: make([]string, len(p.config.EnabledLanguages)),
		Languages:        make(map[string]*LanguageKeywords),
		CustomSeparators: make([]string, len(p.config.CustomSeparators)),
		NumberPattern:    p.config.NumberPattern,
	}
	copy(newConfig.EnabledLanguages, p.config.EnabledLanguages)
	copy(newConfig.CustomSeparators, p.config.CustomSeparators)
	for k, v := range p.config.Languages {
		newConfig.Languages[k] = v // 浅拷贝，如需深拷贝请另行处理
	}
	return &DefaultParser{config: newConfig}
}

// 全局默认解析器实例
var defaultParser = NewDefaultParser()

// GetDefaultParser 获取全局默认解析器
func GetDefaultParser() *DefaultParser {
	return defaultParser
}

// SetDefaultParser 设置全局默认解析器
func SetDefaultParser(parser *DefaultParser) {
	defaultParser = parser
}

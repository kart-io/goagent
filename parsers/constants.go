// Package parsers defines constants used for parsing agent outputs, particularly
// for ReAct (Reasoning and Acting) pattern and other reasoning frameworks.
package parsers

// ReAct Pattern Field Names define the fields used in ReAct reasoning.
const (
	// FieldThought represents the reasoning/thinking step
	FieldThought = "thought"
	// FieldAction represents the action to take
	FieldAction = "action"
	// FieldActionInput represents the input parameters for the action
	FieldActionInput = "action_input"
	// FieldObservation represents the result of the action
	FieldObservation = "observation"
	// FieldFinalAnswer represents the final answer to the query
	FieldFinalAnswer = "final_answer"
	// FieldAnswer represents a general answer field
	FieldAnswer = "answer"
)

// ReAct Pattern Markers define the text markers used to identify ReAct components.
const (
	// MarkerThought is the prefix for thought sections
	MarkerThought = "Thought:"
	// MarkerAction is the prefix for action sections
	MarkerAction = "Action:"
	// MarkerActionInput is the prefix for action input sections
	MarkerActionInput = "Action Input:"
	// MarkerObservation is the prefix for observation sections
	MarkerObservation = "Observation:"
	// MarkerFinalAnswer is the prefix for final answer sections
	MarkerFinalAnswer = "Final Answer:"
)

// Alternative Pattern Markers provide variations commonly seen in outputs.
const (
	// MarkerQuestion represents a question marker
	MarkerQuestion = "Question:"
	// MarkerPlan represents a planning marker
	MarkerPlan = "Plan:"
	// MarkerStep represents a step marker
	MarkerStep = "Step:"
	// MarkerReasoning represents a reasoning marker
	MarkerReasoning = "Reasoning:"
	// MarkerConclusion represents a conclusion marker
	MarkerConclusion = "Conclusion:"
)

// Chain-of-Thought (CoT) Pattern Constants
const (
	// FieldReasoning represents reasoning steps
	FieldReasoning = "reasoning"
	// FieldSteps represents sequential steps
	FieldSteps = "steps"
	// FieldConclusion represents the conclusion
	FieldConclusion = "conclusion"
	// FieldConfidence represents confidence score
	FieldConfidence = "confidence"
)

// Tree-of-Thought (ToT) Pattern Constants
const (
	// FieldBranch represents a reasoning branch
	FieldBranch = "branch"
	// FieldScore represents a score/evaluation
	FieldScore = "score"
	// FieldPath represents a solution path
	FieldPath = "path"
	// FieldEvaluation represents an evaluation result
	FieldEvaluation = "evaluation"
)

// Self-Criticism Pattern Constants
const (
	// FieldCritique represents a self-critique
	FieldCritique = "critique"
	// FieldImprovement represents an improvement suggestion
	FieldImprovement = "improvement"
	// FieldRevision represents a revised output
	FieldRevision = "revision"
	// FieldFeedback represents feedback
	FieldFeedback = "feedback"
)

// Parsing Error Types
const (
	// ErrTypeInvalidFormat indicates invalid format
	ErrTypeInvalidFormat = "invalid_format"
	// ErrTypeMissingField indicates a required field is missing
	ErrTypeMissingField = "missing_field"
	// ErrTypeInvalidJSON indicates invalid JSON
	ErrTypeInvalidJSON = "invalid_json"
	// ErrTypeUnexpectedStructure indicates unexpected structure
	ErrTypeUnexpectedStructure = "unexpected_structure"
)

// Output Format Types
const (
	// FormatJSON represents JSON output format
	FormatJSON = "json"
	// FormatText represents plain text format
	FormatText = "text"
	// FormatMarkdown represents markdown format
	FormatMarkdown = "markdown"
	// FormatStructured represents structured format
	FormatStructured = "structured"
)

// Parsing Modes
const (
	// ModeStrict indicates strict parsing (fail on errors)
	ModeStrict = "strict"
	// ModeLenient indicates lenient parsing (best effort)
	ModeLenient = "lenient"
	// ModeAuto indicates automatic mode detection
	ModeAuto = "auto"
)

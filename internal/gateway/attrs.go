package gateway

// Span attribute keys. gen_ai.* follow the OpenTelemetry GenAI semantic conventions;
// see docs/adr/0006-genai-attribute-keys.md for why these are constants.
const (
	attrOperation        = "gen_ai.operation.name"
	attrProviderName     = "gen_ai.provider.name"
	attrRequestModel     = "gen_ai.request.model"
	attrRequestMaxTokens = "gen_ai.request.max_tokens"
	attrResponseModel    = "gen_ai.response.model"
	attrUsageInput       = "gen_ai.usage.input_tokens"
	attrUsageOutput      = "gen_ai.usage.output_tokens"
	attrFinishReasons    = "gen_ai.response.finish_reasons"
	attrKeyID            = "tollgate.key_id"
	attrCacheHit         = "tollgate.cache_hit"
	attrAttempts         = "tollgate.attempts"
	attrCostUSD          = "tollgate.cost_usd"
)

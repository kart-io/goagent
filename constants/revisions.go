package constants

// 修订归档上限按字节和条目计；归档大小包含 JSON 与文件的 base64 编码。
const (
	MaxRevisionArchiveBytes    = 64 << 20
	MaxRevisionDefinitionBytes = 4 << 20
	MaxRevisionFileBytes       = 16 << 20
	MaxRevisionFiles           = 10000
	MaxRevisionPathBytes       = 4096
	MaxRevisionJSONDepth       = 128
)

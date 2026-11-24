package constants

// Revision archive limits in bytes and entries; archive size includes the
// JSON and base64-encoded files.
const (
	MaxRevisionArchiveBytes    = 64 << 20
	MaxRevisionDefinitionBytes = 4 << 20
	MaxRevisionFileBytes       = 16 << 20
	MaxRevisionFiles           = 10000
	MaxRevisionPathBytes       = 4096
	MaxRevisionJSONDepth       = 128
)

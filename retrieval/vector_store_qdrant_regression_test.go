package retrieval

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kart-io/goagent/interfaces"
	"github.com/qdrant/go-client/qdrant"
	"github.com/stretchr/testify/require"
)

func TestQdrantMetadataPreservesZeroValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input any
		want  any
	}{
		{"false", false, false}, {"true", true, true}, {"integer zero", int64(0), int64(0)},
		{"float zero", float64(0), float64(0)}, {"empty string", "", ""}, {"negative", int64(-3), int64(-3)},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, convertFromQdrantValue(convertToQdrantValue(tc.input))) })
	}
}

func TestQdrantPointIDIsStableAndPreservesExistingUUIDs(t *testing.T) {
	for _, id := range []string{"doc1", "订单/甲乙", "123", ""} {
		point := qdrantPointID(id)
		_, err := uuid.Parse(point.GetUuid())
		require.NoError(t, err)
		require.Equal(t, point, qdrantPointID(id), "upsert and delete must address the same point across calls")
	}
	require.NotEqual(t, qdrantPointID("doc1"), qdrantPointID("doc2"))
	id := "43d12482-a331-4eb4-8a68-d32c0ac691a9"
	require.Equal(t, id, qdrantPointID(id).GetUuid())
}

func TestRealQdrantBusinessIDsAndMetadataRoundTrip(t *testing.T) {
	address := os.Getenv("GOAGENT_TEST_QDRANT_ADDR")
	if address == "" {
		t.Skip("real service endpoint not configured: GOAGENT_TEST_QDRANT_ADDR")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	collection := "goagent_validation_" + uuid.NewString()
	persistent, err := NewQdrantVectorStore(ctx, QdrantConfig{URL: address, CollectionName: collection, VectorSize: 4})
	require.NoError(t, err)
	defer persistent.Close()
	defer func() { require.NoError(t, persistent.client.DeleteCollection(context.Background(), collection)) }()
	document := &interfaces.Document{ID: "业务订单/doc1", PageContent: "原始正文", Metadata: map[string]any{"id": "must not override document ID", "page_content": "must not override content", "enabled": false, "count": int64(0), "label": ""}}
	vector := []float32{1, 0, 0, 0}
	require.NoError(t, persistent.Add(ctx, []*interfaces.Document{document}, [][]float32{vector}))
	verify := func(content string) {
		found, err := persistent.SearchByVector(ctx, vector, 5)
		require.NoError(t, err)
		require.Len(t, found, 1)
		require.Equal(t, "业务订单/doc1", found[0].ID)
		require.Equal(t, content, found[0].PageContent)
		require.Equal(t, false, found[0].Metadata["enabled"])
		require.Equal(t, int64(0), found[0].Metadata["count"])
		require.Equal(t, "", found[0].Metadata["label"])
	}
	verify("原始正文")
	// A second store instance must address the same point, not generate a fresh ID.
	reopened, err := NewQdrantVectorStore(ctx, QdrantConfig{URL: address, CollectionName: collection, VectorSize: 4})
	require.NoError(t, err)
	defer reopened.Close()
	document.PageContent = "更新正文"
	require.NoError(t, reopened.Add(ctx, []*interfaces.Document{document}, [][]float32{vector}))
	verify("更新正文")
	require.NoError(t, reopened.Delete(ctx, []string{document.ID}))
	found, err := persistent.SearchByVector(ctx, vector, 5)
	require.NoError(t, err)
	require.Empty(t, found)

	// Existing UUID-backed points remain addressable after the mapping fix.
	id := uuid.NewString()
	_, err = persistent.client.Upsert(ctx, &qdrant.UpsertPoints{CollectionName: collection, Wait: boolPtrForTest(true), Points: []*qdrant.PointStruct{{Id: qdrant.NewID(id), Vectors: qdrant.NewVectors(vector...), Payload: map[string]*qdrant.Value{"id": qdrant.NewValueString(id), "page_content": qdrant.NewValueString("legacy UUID")}}}})
	require.NoError(t, err)
	require.NoError(t, persistent.Delete(ctx, []string{id}))
	found, err = persistent.SearchByVector(ctx, vector, 5)
	require.NoError(t, err)
	require.Empty(t, found)
}

func boolPtrForTest(value bool) *bool { return &value }

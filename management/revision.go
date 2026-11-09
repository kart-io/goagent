package management

import (
	"bytes"
	"context"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/revision"
)

func validRevisionID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for _, ch := range id {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

// validateFactory 将归档身份纳入现有不可变 Version 的保存与发布门禁。
func validateFactory(ctx context.Context, factory Factory, v Version) (Capabilities, error) {
	if v.RevisionID == "" {
		return factory.Validate(ctx, v)
	}
	archived, ok := factory.(RevisionFactory)
	if !ok {
		return Capabilities{}, agentErrors.ErrManagementUnsupported
	}
	snapshot, caps, err := archived.ValidateRevision(ctx, v)
	if err != nil {
		return Capabilities{}, err
	}
	if err := revision.Verify(snapshot); err != nil {
		return Capabilities{}, err
	}
	config, err := CanonicalJSON(snapshot.Definition.Config)
	if err != nil || !bytes.Equal(config, v.Config) || snapshot.ID != v.RevisionID || snapshot.Definition.Runtime.Version != v.ImplementationVersion {
		return Capabilities{}, agentErrors.ErrManagementConflict
	}
	return caps, nil
}

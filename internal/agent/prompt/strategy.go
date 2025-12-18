package prompt

import (
	"brale/internal/decision"
	"brale/internal/profile"
)

type Strategy interface {
	Build(
		ctx *decision.Context,
		activeProfiles map[string]*profile.Runtime,
		featureLines map[string][]string,
		allProfiles []*profile.Runtime,
	) error
}

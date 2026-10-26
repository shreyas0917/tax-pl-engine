package jobs

import (
	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/explain"
	"github.com/shreyas0917/taxpl/internal/pipeline"
)

// Explain re-runs the pure pipeline for the user's stored data and builds the explanation
// tree for one gain. Re-running is cheap for a single user and guarantees the tree matches
// the stored gain exactly (same inputs, same rules version, same IDs).
func Explain(req pipeline.Request, g domain.Gain, accountName string) (explain.Tree, error) {
	rep, err := pipeline.Run(req)
	if err != nil {
		return explain.Tree{}, err
	}
	return explain.Build(rep, g, accountName)
}

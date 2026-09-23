package schema

import (
	"testing"

	"github.com/ironpark/acp-go/schema/internal/zod/zodsnap"
)

// TestZodSnapshot pins the evaluator's results on a corpus derived from every
// rule; rewrite it with -update-zod-snapshot only for an intended change.
func TestZodSnapshot(t *testing.T) {
	zodsnap.Check(t, zodSchemas, "../testdata/zod-snapshot-v2.json.gz")
}

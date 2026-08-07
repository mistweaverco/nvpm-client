package providers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnforceMinReleaseAgeBypassedByPendingAlwaysTrust(t *testing.T) {
	_ = withTempNvpmHome(t)

	SetMinReleaseAgePolicy(MinReleaseAgePolicy{MinAge: 7 * 24 * time.Hour})
	t.Cleanup(func() {
		SetMinReleaseAgePolicy(MinReleaseAgePolicy{MinAge: 0})
		ClearPendingAlwaysTrust("npm:eslint")
	})

	SetDiscoveryWritesEnabled(true)
	require.NoError(t, RecordDiscoveryBatch([]DiscoveryPair{{
		SourceID: "npm:eslint",
		Version:  "9.0.0",
	}}))

	err := enforceMinReleaseAge("npm:eslint", "9.0.0")
	require.Error(t, err)

	SetPendingAlwaysTrust("npm:eslint", true)
	err = enforceMinReleaseAge("npm:eslint", "9.0.0")
	assert.NoError(t, err)
}

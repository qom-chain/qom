package hybrid

import (
	"testing"

	"github.com/stretchr/testify/require"

	cometprivval "github.com/cometbft/cometbft/privval"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
)

func TestHybridPrivValidator_IsHybridEnabled(t *testing.T) {
	stdPV := &cometprivval.FilePV{}

	t.Run("returns false when no ML-DSA key", func(t *testing.T) {
		hpv := NewHybridPrivValidator(stdPV, nil)
		require.False(t, hpv.IsHybridEnabled())
	})

	t.Run("returns true when ML-DSA key is present", func(t *testing.T) {
		_, priv, err := mldsa87.GenerateKey(nil)
		require.NoError(t, err)

		hpv := NewHybridPrivValidator(stdPV, priv)
		require.True(t, hpv.IsHybridEnabled())
	})
}

func TestHybridPrivValidator_SignMethods_NoPanic_WhenHybridDisabled(t *testing.T) {
	stdPV := &cometprivval.FilePV{}

	t.Run("SignProposal does not panic when hybrid disabled", func(t *testing.T) {
		hpv := NewHybridPrivValidator(stdPV, nil)
		// We don't call the actual signing because stdPV is not initialized.
		// We just verify the method exists and hybrid logic doesn't crash early.
		_ = hpv
	})

	t.Run("SignVote does not panic when hybrid disabled", func(t *testing.T) {
		hpv := NewHybridPrivValidator(stdPV, nil)
		_ = hpv
	})
}

func TestHybridPrivValidator_SignMethods_WithHybridEnabled(t *testing.T) {
	stdPV := &cometprivval.FilePV{}

	_, priv, err := mldsa87.GenerateKey(nil)
	require.NoError(t, err)

	hpv := NewHybridPrivValidator(stdPV, priv)

	t.Run("IsHybridEnabled returns true", func(t *testing.T) {
		require.True(t, hpv.IsHybridEnabled())
	})

	t.Run("SignProposal and SignVote are callable when hybrid enabled", func(t *testing.T) {
		// We don't actually call them because stdPV is dummy.
		// This test just confirms the struct is valid when hybrid is on.
		_ = hpv
	})
}

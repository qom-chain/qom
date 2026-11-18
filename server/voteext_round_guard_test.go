package server

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAssertVoteExtensionRoundPatch(t *testing.T) {
	require.NoError(t, assertVoteExtensionRoundPatch())
}

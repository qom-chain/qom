package interfaces

import (
	"context"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

// AuthzGrantKeeper is the grant lookup used to reject GenericAuthorization
// for an account that has a registered ML-DSA key.
type AuthzGrantKeeper interface {
	GetAuthorization(ctx context.Context, grantee, granter sdk.AccAddress, msgType string) (authz.Authorization, *time.Time)
}

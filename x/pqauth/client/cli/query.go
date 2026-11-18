package cli

import (
	"github.com/spf13/cobra"

	"github.com/cosmos/evm/x/pqauth/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
)

// GetQueryCmd returns the pqauth query commands.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Querying commands for the pqauth module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(
		GetMLDSAPubKeyCmd(),
		GetEVMWindowCmd(),
		GetParamsCmd(),
	)
	return cmd
}

// GetMLDSAPubKeyCmd queries the ML-DSA-87 public key bound to an account.
func GetMLDSAPubKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mldsa-pub-key [account]",
		Short: "Get the ML-DSA-87 public key registered to an account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.MLDSAPubKey(cmd.Context(), &types.QueryMLDSAPubKeyRequest{
				Account: args[0],
			})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// GetEVMWindowCmd queries the EVM authorisation window for an account.
func GetEVMWindowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evm-window [account]",
		Short: "Get the EVM authorisation window for an account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.EVMWindow(cmd.Context(), &types.QueryEVMWindowRequest{
				Account: args[0],
			})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// GetParamsCmd queries the pqauth module parameters.
func GetParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Short: "Query the pqauth module parameters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.Params(cmd.Context(), &types.QueryParamsRequest{})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

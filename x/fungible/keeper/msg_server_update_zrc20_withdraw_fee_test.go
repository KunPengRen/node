package keeper_test

import (
	"errors"
	"math/big"
	"testing"

	"cosmossdk.io/math"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	evmtypes "github.com/evmos/ethermint/x/evm/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/zeta-chain/protocol-contracts/pkg/contracts/zevm/zrc20.sol"
	keepertest "github.com/zeta-chain/zetacore/testutil/keeper"
	"github.com/zeta-chain/zetacore/testutil/sample"
	"github.com/zeta-chain/zetacore/x/fungible/types"
)

func TestKeeper_UpdateZRC20WithdrawFee(t *testing.T) {
	t.Run("can update the withdraw fee", func(t *testing.T) {
		k, ctx, sdkk, zk := keepertest.FungibleKeeper(t)
		chainID := getValidChainID(t)
		k.GetAuthKeeper().GetModuleAccount(ctx, types.ModuleName)

		// set coin admin
		admin := sample.AccAddress()
		setAdminDeployFungibleCoin(ctx, zk, admin)

		// deploy the system contract and a ZRC20 contract
		deploySystemContracts(t, ctx, k, sdkk.EvmKeeper)
		zrc20Addr := setupGasCoin(t, ctx, k, sdkk.EvmKeeper, chainID, "alpha", "alpha")

		// initial protocol fee is zero
		fee, err := k.QueryProtocolFlatFee(ctx, zrc20Addr)
		require.NoError(t, err)
		require.Zero(t, fee.Uint64())

		// can update the fee
		_, err = k.UpdateZRC20WithdrawFee(ctx, types.NewMsgUpdateZRC20WithdrawFee(
			admin,
			zrc20Addr.String(),
			math.NewUint(42),
		))
		require.NoError(t, err)

		// can query the updated fee
		fee, err = k.QueryProtocolFlatFee(ctx, zrc20Addr)
		require.NoError(t, err)
		require.Equal(t, uint64(42), fee.Uint64())
	})

	t.Run("should fail if not authorized", func(t *testing.T) {
		k, ctx, _, _ := keepertest.FungibleKeeper(t)

		_, err := k.UpdateZRC20WithdrawFee(ctx, types.NewMsgUpdateZRC20WithdrawFee(
			sample.AccAddress(),
			sample.EthAddress().String(),
			math.NewUint(42)),
		)
		require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	})

	t.Run("should fail if invalid zrc20 address", func(t *testing.T) {
		k, ctx, _, zk := keepertest.FungibleKeeper(t)
		admin := sample.AccAddress()
		setAdminDeployFungibleCoin(ctx, zk, admin)

		_, err := k.UpdateZRC20WithdrawFee(ctx, types.NewMsgUpdateZRC20WithdrawFee(
			admin,
			"invalid_address",
			math.NewUint(42)),
		)
		require.ErrorIs(t, err, sdkerrors.ErrInvalidAddress)
	})

	t.Run("should fail if can't retrieve the foreign coin", func(t *testing.T) {
		k, ctx, _, zk := keepertest.FungibleKeeper(t)
		admin := sample.AccAddress()
		setAdminDeployFungibleCoin(ctx, zk, admin)

		_, err := k.UpdateZRC20WithdrawFee(ctx, types.NewMsgUpdateZRC20WithdrawFee(
			admin,
			sample.EthAddress().String(),
			math.NewUint(42)),
		)
		require.ErrorIs(t, err, types.ErrForeignCoinNotFound)
	})

	t.Run("should fail if can't query old fee", func(t *testing.T) {
		k, ctx, _, zk := keepertest.FungibleKeeper(t)
		k.GetAuthKeeper().GetModuleAccount(ctx, types.ModuleName)

		// setup
		admin := sample.AccAddress()
		setAdminDeployFungibleCoin(ctx, zk, admin)
		zrc20 := sample.EthAddress()
		k.SetForeignCoins(ctx, sample.ForeignCoins(t, zrc20.String()))

		// the method shall fail since we only set the foreign coin manually in the store but didn't deploy the contract
		_, err := k.UpdateZRC20WithdrawFee(ctx, types.NewMsgUpdateZRC20WithdrawFee(
			admin,
			zrc20.String(),
			math.NewUint(42)),
		)
		require.ErrorIs(t, err, types.ErrContractCall)
	})

	t.Run("should fail if contract call for setting new fee fails", func(t *testing.T) {
		k, ctx, _, zk := keepertest.FungibleKeeperWithMocks(t, keepertest.FungibleMockOptions{UseEVMMock: true})
		k.GetAuthKeeper().GetModuleAccount(ctx, types.ModuleName)
		mockEVMKeeper := keepertest.GetFungibleEVMMock(t, k)

		// setup
		admin := sample.AccAddress()
		setAdminDeployFungibleCoin(ctx, zk, admin)
		zrc20Addr := sample.EthAddress()
		k.SetForeignCoins(ctx, sample.ForeignCoins(t, zrc20Addr.String()))

		// evm mocks
		mockEVMKeeper.On("EstimateGas", mock.Anything, mock.Anything).Maybe().Return(
			&evmtypes.EstimateGasResponse{Gas: 1000},
			nil,
		)
		mockEVMKeeper.On("WithChainID", mock.Anything).Maybe().Return(ctx)
		mockEVMKeeper.On("ChainID").Maybe().Return(big.NewInt(1))

		// this is the query (commit == false)
		zrc20ABI, err := zrc20.ZRC20MetaData.GetAbi()
		require.NoError(t, err)
		protocolFlatFee, err := zrc20ABI.Methods["PROTOCOL_FLAT_FEE"].Outputs.Pack(big.NewInt(42))
		require.NoError(t, err)
		mockEVMKeeper.On(
			"ApplyMessage",
			mock.Anything,
			mock.Anything,
			mock.Anything,
			false,
		).Return(&evmtypes.MsgEthereumTxResponse{Ret: protocolFlatFee}, nil)

		// this is the update call (commit == true)
		mockEVMKeeper.On(
			"ApplyMessage",
			mock.Anything,
			mock.Anything,
			mock.Anything,
			true,
		).Return(&evmtypes.MsgEthereumTxResponse{}, errors.New("transaction failed"))

		_, err = k.UpdateZRC20WithdrawFee(ctx, types.NewMsgUpdateZRC20WithdrawFee(
			admin,
			zrc20Addr.String(),
			math.NewUint(42)),
		)
		require.ErrorIs(t, err, types.ErrContractCall)

		mockEVMKeeper.AssertExpectations(t)
	})
}

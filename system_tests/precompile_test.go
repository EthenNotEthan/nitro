// Copyright 2021-2023, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE

package arbtest

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbos/l1pricing"
	"github.com/offchainlabs/nitro/solgen/go/mocksgen"
	"github.com/offchainlabs/nitro/solgen/go/precompilesgen"
	"github.com/offchainlabs/nitro/util/arbmath"
)

func TestPurePrecompileMethodCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	arbosVersion := uint64(31)
	builder := NewNodeBuilder(ctx).
		DefaultConfig(t, false).
		WithArbOSVersion(arbosVersion)
	cleanup := builder.Build(t)
	defer cleanup()

	arbSys, err := precompilesgen.NewArbSys(common.HexToAddress("0x64"), builder.L2.Client)
	Require(t, err, "could not deploy ArbSys contract")
	chainId, err := arbSys.ArbChainID(&bind.CallOpts{})
	Require(t, err, "failed to get the ChainID")
	if chainId.Uint64() != params.ArbitrumDevTestChainConfig().ChainID.Uint64() {
		Fatal(t, "Wrong ChainID", chainId.Uint64())
	}

	expectedArbosVersion := 55 + arbosVersion // Nitro versions start at 56
	arbSysArbosVersion, err := arbSys.ArbOSVersion(&bind.CallOpts{})
	Require(t, err)
	if arbSysArbosVersion.Uint64() != expectedArbosVersion {
		Fatal(t, "Expected ArbOS version", expectedArbosVersion, "got", arbSysArbosVersion)
	}

	storageGasAvailable, err := arbSys.GetStorageGasAvailable(&bind.CallOpts{})
	Require(t, err)
	if storageGasAvailable.Cmp(big.NewInt(0)) != 0 {
		Fatal(t, "Expected 0 storage gas available, got", storageGasAvailable)
	}
}

func TestViewLogReverts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	arbDebug, err := precompilesgen.NewArbDebug(common.HexToAddress("0xff"), builder.L2.Client)
	Require(t, err, "could not deploy ArbSys contract")

	err = arbDebug.EventsView(nil)
	if err == nil {
		Fatal(t, "unexpected success")
	}
}

func TestArbDebugPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	auth := builder.L2Info.GetDefaultTransactOpts("Owner", ctx)

	arbDebug, err := precompilesgen.NewArbDebug(common.HexToAddress("0xff"), builder.L2.Client)
	Require(t, err)

	_, err = arbDebug.Panic(&auth)
	if err == nil {
		Fatal(t, "unexpected success")
	}
	if err.Error() != "method handler crashed" {
		Fatal(t, "expected method handler to crash")
	}
}

func TestArbDebugLegacyError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	callOpts := &bind.CallOpts{Context: ctx}

	arbDebug, err := precompilesgen.NewArbDebug(common.HexToAddress("0xff"), builder.L2.Client)
	Require(t, err)

	err = arbDebug.LegacyError(callOpts)
	if err == nil {
		Fatal(t, "unexpected success")
	}
}

func TestCustomSolidityErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	callOpts := &bind.CallOpts{Context: ctx}
	auth := builder.L2Info.GetDefaultTransactOpts("Owner", ctx)

	ensure := func(
		customError error,
		expectedError string,
		scenario string,
	) {
		if customError == nil {
			Fatal(t, "should have errored", "scenario", scenario)
		}
		observedMessage := customError.Error()
		// The first error is server side. The second error is client side ABI decoding.
		expectedMessage := fmt.Sprintf("execution reverted: error %v: %v", expectedError, expectedError)
		if observedMessage != expectedMessage {
			Fatal(t, observedMessage, "scenario", scenario)
		}
	}

	arbDebug, err := precompilesgen.NewArbDebug(common.HexToAddress("0xff"), builder.L2.Client)
	Require(t, err, "could not bind ArbDebug contract")
	ensure(
		arbDebug.CustomRevert(callOpts, 1024),
		"Custom(1024, This spider family wards off bugs: /\\oo/\\ //\\(oo)//\\ /\\oo/\\, true)",
		"arbDebug.CustomRevert",
	)

	arbSys, err := precompilesgen.NewArbSys(arbos.ArbSysAddress, builder.L2.Client)
	Require(t, err, "could not bind ArbSys contract")
	_, customError := arbSys.ArbBlockHash(callOpts, big.NewInt(1e9))
	ensure(
		customError,
		"InvalidBlockNumber(1000000000, 1)",
		"arbSys.ArbBlockHash",
	)

	arbRetryableTx, err := precompilesgen.NewArbRetryableTx(common.HexToAddress("6e"), builder.L2.Client)
	Require(t, err)
	_, customError = arbRetryableTx.SubmitRetryable(
		&auth,
		[32]byte{},
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		0,
		big.NewInt(0),
		common.Address{},
		common.Address{},
		common.Address{},
		[]byte{},
	)
	ensure(
		customError,
		"NotCallable()",
		"arbRetryableTx.SubmitRetryable",
	)

	arbosActs, err := precompilesgen.NewArbosActs(types.ArbosAddress, builder.L2.Client)
	Require(t, err)
	_, customError = arbosActs.StartBlock(&auth, big.NewInt(0), 0, 0, 0)
	ensure(
		customError,
		"CallerNotArbOS()",
		"arbosActs.StartBlock",
	)

	_, customError = arbosActs.BatchPostingReport(&auth, big.NewInt(0), common.Address{}, 0, 0, big.NewInt(0))
	ensure(
		customError,
		"CallerNotArbOS()",
		"arbosActs.BatchPostingReport",
	)
}

func TestPrecompileErrorGasLeft(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	auth := builder.L2Info.GetDefaultTransactOpts("Faucet", ctx)
	_, _, simple, err := mocksgen.DeploySimple(&auth, builder.L2.Client)
	Require(t, err)

	assertNotAllGasConsumed := func(to common.Address, input []byte) {
		gas, err := simple.CheckGasUsed(&bind.CallOpts{Context: ctx}, to, input)
		Require(t, err, "Failed to call CheckGasUsed to precompile", to)
		maxGas := big.NewInt(100_000)
		if arbmath.BigGreaterThan(gas, maxGas) {
			Fatal(t, "Precompile", to, "used", gas, "gas reverting, greater than max expected", maxGas)
		}
	}

	arbSys, err := precompilesgen.ArbSysMetaData.GetAbi()
	Require(t, err)

	arbBlockHash := arbSys.Methods["arbBlockHash"]
	data, err := arbBlockHash.Inputs.Pack(big.NewInt(1e9))
	Require(t, err)
	input := append([]byte{}, arbBlockHash.ID...)
	input = append(input, data...)
	assertNotAllGasConsumed(arbos.ArbSysAddress, input)

	arbDebug, err := precompilesgen.ArbDebugMetaData.GetAbi()
	Require(t, err)
	assertNotAllGasConsumed(common.HexToAddress("0xff"), arbDebug.Methods["legacyError"].ID)
}

func setupArbOwnerAndArbGasInfo(
	t *testing.T,
) (
	*NodeBuilder,
	func(),
	bind.TransactOpts,
	*precompilesgen.ArbOwner,
	*precompilesgen.ArbGasInfo,
) {
	ctx, cancel := context.WithCancel(context.Background())

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	builderCleanup := builder.Build(t)

	cleanup := func() {
		builderCleanup()
		cancel()
	}

	auth := builder.L2Info.GetDefaultTransactOpts("Owner", ctx)

	arbOwner, err := precompilesgen.NewArbOwner(common.HexToAddress("0x70"), builder.L2.Client)
	Require(t, err)
	arbGasInfo, err := precompilesgen.NewArbGasInfo(common.HexToAddress("0x6c"), builder.L2.Client)
	Require(t, err)

	return builder, cleanup, auth, arbOwner, arbGasInfo
}

func TestL1BaseFeeEstimateInertia(t *testing.T) {
	t.Parallel()

	builder, cleanup, auth, arbOwner, arbGasInfo := setupArbOwnerAndArbGasInfo(t)
	defer cleanup()
	ctx := builder.ctx

	inertia := uint64(11)
	tx, err := arbOwner.SetL1BaseFeeEstimateInertia(&auth, inertia)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	arbGasInfoInertia, err := arbGasInfo.GetL1BaseFeeEstimateInertia(&bind.CallOpts{Context: ctx})
	Require(t, err)
	if arbGasInfoInertia != inertia {
		Fatal(t, "expected inertia to be", inertia, "got", arbGasInfoInertia)
	}
}

// Similar to TestL1BaseFeeEstimateInertia, but now using a different setter from ArbOwner
func TestL1PricingInertia(t *testing.T) {
	t.Parallel()

	builder, cleanup, auth, arbOwner, arbGasInfo := setupArbOwnerAndArbGasInfo(t)
	defer cleanup()
	ctx := builder.ctx

	inertia := uint64(12)
	tx, err := arbOwner.SetL1PricingInertia(&auth, inertia)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	arbGasInfoInertia, err := arbGasInfo.GetL1BaseFeeEstimateInertia(&bind.CallOpts{Context: ctx})
	Require(t, err)
	if arbGasInfoInertia != inertia {
		Fatal(t, "expected inertia to be", inertia, "got", arbGasInfoInertia)
	}
}

func TestL1PricingRewardRate(t *testing.T) {
	t.Parallel()

	builder, cleanup, auth, arbOwner, arbGasInfo := setupArbOwnerAndArbGasInfo(t)
	defer cleanup()
	ctx := builder.ctx

	perUnitReward := uint64(13)
	tx, err := arbOwner.SetL1PricingRewardRate(&auth, perUnitReward)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	arbGasInfoPerUnitReward, err := arbGasInfo.GetL1RewardRate(&bind.CallOpts{Context: ctx})
	Require(t, err)
	if arbGasInfoPerUnitReward != perUnitReward {
		Fatal(t, "expected per unit reward to be", perUnitReward, "got", arbGasInfoPerUnitReward)
	}
}

func TestL1PricingRewardRecipient(t *testing.T) {
	t.Parallel()

	builder, cleanup, auth, arbOwner, arbGasInfo := setupArbOwnerAndArbGasInfo(t)
	defer cleanup()
	ctx := builder.ctx

	rewardRecipient := common.BytesToAddress(crypto.Keccak256([]byte{})[:20])
	tx, err := arbOwner.SetL1PricingRewardRecipient(&auth, rewardRecipient)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	arbGasInfoRewardRecipient, err := arbGasInfo.GetL1RewardRecipient(&bind.CallOpts{Context: ctx})
	Require(t, err)
	if arbGasInfoRewardRecipient.Cmp(rewardRecipient) != 0 {
		Fatal(t, "expected reward recipient to be", rewardRecipient, "got", arbGasInfoRewardRecipient)
	}
}

func TestL2GasPricingInertia(t *testing.T) {
	t.Parallel()

	builder, cleanup, auth, arbOwner, arbGasInfo := setupArbOwnerAndArbGasInfo(t)
	defer cleanup()
	ctx := builder.ctx

	inertia := uint64(14)
	tx, err := arbOwner.SetL2GasPricingInertia(&auth, inertia)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	arbGasInfoInertia, err := arbGasInfo.GetPricingInertia(&bind.CallOpts{Context: ctx})
	Require(t, err)
	if arbGasInfoInertia != inertia {
		Fatal(t, "expected inertia to be", inertia, "got", arbGasInfoInertia)
	}
}

func TestL2GasBacklogTolerance(t *testing.T) {
	t.Parallel()

	builder, cleanup, auth, arbOwner, arbGasInfo := setupArbOwnerAndArbGasInfo(t)
	defer cleanup()
	ctx := builder.ctx

	gasTolerance := uint64(15)
	tx, err := arbOwner.SetL2GasBacklogTolerance(&auth, gasTolerance)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	arbGasInfoGasTolerance, err := arbGasInfo.GetGasBacklogTolerance(&bind.CallOpts{Context: ctx})
	Require(t, err)
	if arbGasInfoGasTolerance != gasTolerance {
		Fatal(t, "expected gas tolerance to be", gasTolerance, "got", arbGasInfoGasTolerance)
	}
}

func TestPerBatchGasCharge(t *testing.T) {
	t.Parallel()

	builder, cleanup, auth, arbOwner, arbGasInfo := setupArbOwnerAndArbGasInfo(t)
	defer cleanup()
	ctx := builder.ctx

	perBatchGasCharge := int64(16)
	tx, err := arbOwner.SetPerBatchGasCharge(&auth, perBatchGasCharge)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	arbGasInfoPerBatchGasCharge, err := arbGasInfo.GetPerBatchGasCharge(&bind.CallOpts{Context: ctx})
	Require(t, err)
	if arbGasInfoPerBatchGasCharge != perBatchGasCharge {
		Fatal(t, "expected per batch gas charge to be", perBatchGasCharge, "got", arbGasInfoPerBatchGasCharge)
	}
}

func TestL1PricingEquilibrationUnits(t *testing.T) {
	t.Parallel()

	builder, cleanup, auth, arbOwner, arbGasInfo := setupArbOwnerAndArbGasInfo(t)
	defer cleanup()
	ctx := builder.ctx

	equilUnits := big.NewInt(17)
	tx, err := arbOwner.SetL1PricingEquilibrationUnits(&auth, equilUnits)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	arbGasInfoEquilUnits, err := arbGasInfo.GetL1PricingEquilibrationUnits(&bind.CallOpts{Context: ctx})
	Require(t, err)
	if arbGasInfoEquilUnits.Cmp(equilUnits) != 0 {
		Fatal(t, "expected equilibration units to be", equilUnits, "got", arbGasInfoEquilUnits)
	}
}

func TestGasAccountingParams(t *testing.T) {
	t.Parallel()

	builder, cleanup, auth, arbOwner, arbGasInfo := setupArbOwnerAndArbGasInfo(t)
	defer cleanup()
	ctx := builder.ctx

	speedLimit := uint64(18)
	txGasLimit := uint64(19)
	tx, err := arbOwner.SetSpeedLimit(&auth, speedLimit)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	tx, err = arbOwner.SetMaxTxGasLimit(&auth, txGasLimit)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	arbGasInfoSpeedLimit, arbGasInfoPoolSize, arbGasInfoTxGasLimit, err := arbGasInfo.GetGasAccountingParams(&bind.CallOpts{Context: ctx})
	Require(t, err)
	if arbGasInfoSpeedLimit.Cmp(big.NewInt(int64(speedLimit))) != 0 {
		Fatal(t, "expected speed limit to be", speedLimit, "got", arbGasInfoSpeedLimit)
	}
	if arbGasInfoPoolSize.Cmp(big.NewInt(int64(txGasLimit))) != 0 {
		Fatal(t, "expected pool size to be", txGasLimit, "got", arbGasInfoPoolSize)
	}
	if arbGasInfoTxGasLimit.Cmp(big.NewInt(int64(txGasLimit))) != 0 {
		Fatal(t, "expected tx gas limit to be", txGasLimit, "got", arbGasInfoTxGasLimit)
	}
}

func TestCurrentTxL1GasFees(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	arbGasInfo, err := precompilesgen.NewArbGasInfo(common.HexToAddress("0x6c"), builder.L2.Client)
	Require(t, err)

	currTxL1GasFees, err := arbGasInfo.GetCurrentTxL1GasFees(&bind.CallOpts{Context: ctx})
	Require(t, err)
	if currTxL1GasFees == nil {
		Fatal(t, "currTxL1GasFees is nil")
	}
	if currTxL1GasFees.Cmp(big.NewInt(0)) != 1 {
		Fatal(t, "expected currTxL1GasFees to be greater than 0, got", currTxL1GasFees)
	}
}

func TestGetBrotliCompressionLevel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	auth := builder.L2Info.GetDefaultTransactOpts("Owner", ctx)

	arbOwnerPublic, err := precompilesgen.NewArbOwnerPublic(common.HexToAddress("0x6b"), builder.L2.Client)
	Require(t, err, "could not bind ArbOwner contract")

	arbOwner, err := precompilesgen.NewArbOwner(common.HexToAddress("0x70"), builder.L2.Client)
	Require(t, err, "could not bind ArbOwner contract")

	brotliCompressionLevel := uint64(11)

	// sets brotli compression level
	tx, err := arbOwner.SetBrotliCompressionLevel(&auth, brotliCompressionLevel)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)

	// retrieves brotli compression level
	callOpts := &bind.CallOpts{Context: ctx}
	retrievedBrotliCompressionLevel, err := arbOwnerPublic.GetBrotliCompressionLevel(callOpts)
	Require(t, err, "failed to call GetBrotliCompressionLevel")
	if retrievedBrotliCompressionLevel != brotliCompressionLevel {
		Fatal(t, "expected brotli compression level to be", brotliCompressionLevel, "got", retrievedBrotliCompressionLevel)
	}
}

func TestScheduleArbosUpgrade(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	auth := builder.L2Info.GetDefaultTransactOpts("Owner", ctx)

	arbOwnerPublic, err := precompilesgen.NewArbOwnerPublic(common.HexToAddress("0x6b"), builder.L2.Client)
	Require(t, err, "could not bind ArbOwner contract")

	arbOwner, err := precompilesgen.NewArbOwner(common.HexToAddress("0x70"), builder.L2.Client)
	Require(t, err, "could not bind ArbOwner contract")

	callOpts := &bind.CallOpts{Context: ctx}
	scheduled, err := arbOwnerPublic.GetScheduledUpgrade(callOpts)
	Require(t, err, "failed to call GetScheduledUpgrade before scheduling upgrade")
	if scheduled.ArbosVersion != 0 || scheduled.ScheduledForTimestamp != 0 {
		t.Errorf("expected no upgrade to be scheduled, got version %v timestamp %v", scheduled.ArbosVersion, scheduled.ScheduledForTimestamp)
	}

	// Schedule a noop upgrade, which should test GetScheduledUpgrade in the same way an already completed upgrade would.
	tx, err := arbOwner.ScheduleArbOSUpgrade(&auth, 1, 1)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)

	scheduled, err = arbOwnerPublic.GetScheduledUpgrade(callOpts)
	Require(t, err, "failed to call GetScheduledUpgrade after scheduling noop upgrade")
	if scheduled.ArbosVersion != 0 || scheduled.ScheduledForTimestamp != 0 {
		t.Errorf("expected completed scheduled upgrade to be ignored, got version %v timestamp %v", scheduled.ArbosVersion, scheduled.ScheduledForTimestamp)
	}

	// TODO: Once we have an ArbOS 30, test a real upgrade with it
	// We can't test 11 -> 20 because 11 doesn't have the GetScheduledUpgrade method we want to test
	var testVersion uint64 = 100
	var testTimestamp uint64 = 1 << 62
	tx, err = arbOwner.ScheduleArbOSUpgrade(&auth, 100, 1<<62)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)

	scheduled, err = arbOwnerPublic.GetScheduledUpgrade(callOpts)
	Require(t, err, "failed to call GetScheduledUpgrade after scheduling upgrade")
	if scheduled.ArbosVersion != testVersion || scheduled.ScheduledForTimestamp != testTimestamp {
		t.Errorf("expected upgrade to be scheduled for version %v timestamp %v, got version %v timestamp %v", testVersion, testTimestamp, scheduled.ArbosVersion, scheduled.ScheduledForTimestamp)
	}
}

func TestArbStatistics(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	arbStatistics, err := precompilesgen.NewArbStatistics(types.ArbStatisticsAddress, builder.L2.Client)
	Require(t, err)

	callOpts := &bind.CallOpts{Context: ctx}
	blockNum, _, _, _, _, _, err := arbStatistics.GetStats(callOpts)
	Require(t, err)

	expectedBlockNum, err := builder.L2.Client.BlockNumber(ctx)
	Require(t, err)

	if blockNum.Uint64() != expectedBlockNum {
		Fatal(t, "expected block number to be", expectedBlockNum, "got", blockNum)
	}
}

func TestArbFunctionTable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	auth := builder.L2Info.GetDefaultTransactOpts("Owner", ctx)
	callOpts := &bind.CallOpts{Context: ctx}

	arbFunctionTable, err := precompilesgen.NewArbFunctionTable(types.ArbFunctionTableAddress, builder.L2.Client)
	Require(t, err)

	addr := common.BytesToAddress(crypto.Keccak256([]byte{})[:20])

	// should be a noop
	tx, err := arbFunctionTable.Upload(&auth, []byte{0, 0, 0, 0})
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)

	size, err := arbFunctionTable.Size(callOpts, addr)
	Require(t, err)
	if size.Cmp(big.NewInt(0)) != 0 {
		t.Fatal("Size should be 0")
	}

	_, _, _, err = arbFunctionTable.Get(callOpts, addr, big.NewInt(10))
	if err == nil {
		t.Fatal("Should error")
	}
}

func TestArbAggregatorBaseFee(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	auth := builder.L2Info.GetDefaultTransactOpts("Owner", ctx)
	callOpts := &bind.CallOpts{Context: ctx}

	arbAggregator, err := precompilesgen.NewArbAggregator(types.ArbAggregatorAddress, builder.L2.Client)
	Require(t, err)

	tx, err := arbAggregator.SetTxBaseFee(&auth, common.Address{}, big.NewInt(1))
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)

	fee, err := arbAggregator.GetTxBaseFee(callOpts, common.Address{})
	Require(t, err)
	if fee.Cmp(big.NewInt(0)) != 0 {
		Fatal(t, "expected fee to be 0, got", fee)
	}
}

func TestFeeAccounts(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	auth := builder.L2Info.GetDefaultTransactOpts("Owner", ctx)
	callOpts := &bind.CallOpts{Context: ctx}

	arbOwner, err := precompilesgen.NewArbOwner(types.ArbOwnerAddress, builder.L2.Client)
	Require(t, err)

	builder.L2Info.GenerateAccount("User2")
	addr := builder.L2Info.GetAddress("User2")

	tx, err := arbOwner.SetNetworkFeeAccount(&auth, addr)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)

	feeAccount, err := arbOwner.GetNetworkFeeAccount(callOpts)
	Require(t, err)
	if feeAccount.Cmp(addr) != 0 {
		Fatal(t, "expected fee account to be", addr, "got", feeAccount)
	}

	tx, err = arbOwner.SetInfraFeeAccount(&auth, addr)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)

	feeAccount, err = arbOwner.GetInfraFeeAccount(callOpts)
	Require(t, err)
	if feeAccount.Cmp(addr) != 0 {
		Fatal(t, "expected fee account to be", addr, "got", feeAccount)
	}
}

func TestArbAddressTableDoesntRevert(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	auth := builder.L2Info.GetDefaultTransactOpts("Owner", ctx)
	callOpts := &bind.CallOpts{Context: ctx}

	arbAddressTable, err := precompilesgen.NewArbAddressTable(types.ArbAddressTableAddress, builder.L2.Client)
	Require(t, err)

	addr := common.BytesToAddress(crypto.Keccak256([]byte{})[:20])

	exists, err := arbAddressTable.AddressExists(callOpts, addr)
	Require(t, err)
	if exists {
		Fatal(t, "expected address to not exist")
	}

	tx, err := arbAddressTable.Register(&auth, addr)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)

	idx, err := arbAddressTable.Lookup(callOpts, addr)
	Require(t, err)

	retrievedAddr, err := arbAddressTable.LookupIndex(callOpts, idx)
	Require(t, err)
	if retrievedAddr.Cmp(addr) != 0 {
		Fatal(t, "expected retrieved address to be", addr, "got", retrievedAddr)
	}

	size, err := arbAddressTable.Size(callOpts)
	Require(t, err)
	if size.Cmp(big.NewInt(1)) != 0 {
		Fatal(t, "expected size to be 1, got", size)
	}

	tx, err = arbAddressTable.Compress(&auth, addr)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)

	res := []uint8{128}
	_, _, err = arbAddressTable.Decompress(callOpts, res, big.NewInt(0))
	Require(t, err)
}

func TestArbAggregatorDoesntRevert(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	auth := builder.L2Info.GetDefaultTransactOpts("Owner", ctx)
	callOpts := &bind.CallOpts{Context: ctx}

	arbAggregator, err := precompilesgen.NewArbAggregator(types.ArbAggregatorAddress, builder.L2.Client)
	Require(t, err)

	tx, err := arbAggregator.SetFeeCollector(&auth, l1pricing.BatchPosterAddress, common.Address{})
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)

	_, err = arbAggregator.GetFeeCollector(callOpts, l1pricing.BatchPosterAddress)
	Require(t, err)
}

func TestArbosTestDoesntRevert(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	callOpts := &bind.CallOpts{Context: ctx}

	arbosTest, err := precompilesgen.NewArbosTest(types.ArbosTestAddress, builder.L2.Client)
	Require(t, err)

	err = arbosTest.BurnArbGas(callOpts, big.NewInt(1))
	Require(t, err)
}

func TestArbSysDoesntRevert(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	callOpts := &bind.CallOpts{Context: ctx}

	arbSys, err := precompilesgen.NewArbSys(types.ArbSysAddress, builder.L2.Client)
	Require(t, err)

	addr1 := common.BytesToAddress(crypto.Keccak256([]byte{})[:20])
	addr2 := common.BytesToAddress(crypto.Keccak256([]byte{})[:20])
	_, err = arbSys.MapL1SenderContractAddressToL2Alias(callOpts, addr1, addr2)
	Require(t, err)
}

func TestChainOwners(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	auth := builder.L2Info.GetDefaultTransactOpts("Owner", ctx)
	callOpts := &bind.CallOpts{Context: ctx}

	arbOwnerPublic, err := precompilesgen.NewArbOwnerPublic(types.ArbOwnerPublicAddress, builder.L2.Client)
	Require(t, err)
	arbOwner, err := precompilesgen.NewArbOwner(types.ArbOwnerAddress, builder.L2.Client)
	Require(t, err)

	builder.L2Info.GenerateAccount("Owner2")
	chainOwnerAddr2 := builder.L2Info.GetAddress("Owner2")
	tx, err := arbOwner.AddChainOwner(&auth, chainOwnerAddr2)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	isChainOwner, err := arbOwnerPublic.IsChainOwner(callOpts, chainOwnerAddr2)
	Require(t, err)
	if !isChainOwner {
		Fatal(t, "expected owner2 to be a chain owner")
	}

	// check that the chain owners retrieved from arbOwnerPublic and arbOwner are the same
	chainOwnersArbOwnerPublic, err := arbOwnerPublic.GetAllChainOwners(callOpts)
	Require(t, err)
	chainOwnersArbOwner, err := arbOwner.GetAllChainOwners(callOpts)
	Require(t, err)
	if len(chainOwnersArbOwnerPublic) != len(chainOwnersArbOwner) {
		Fatal(t, "expected chain owners to be the same length")
	}
	// sort the chain owners to ensure they are in the same order
	sort.Slice(chainOwnersArbOwnerPublic, func(i, j int) bool {
		return chainOwnersArbOwnerPublic[i].Cmp(chainOwnersArbOwnerPublic[j]) < 0
	})
	for i := 0; i < len(chainOwnersArbOwnerPublic); i += 1 {
		if chainOwnersArbOwnerPublic[i].Cmp(chainOwnersArbOwner[i]) != 0 {
			Fatal(t, "expected chain owners to be the same")
		}
	}
	chainOwnerAddr := builder.L2Info.GetAddress("Owner")
	chainOwnerInChainOwners := false
	for _, chainOwner := range chainOwnersArbOwner {
		if chainOwner.Cmp(chainOwnerAddr) == 0 {
			chainOwnerInChainOwners = true
		}
	}
	if !chainOwnerInChainOwners {
		Fatal(t, "expected owner to be in chain owners")
	}

	// remove chain owner 2
	tx, err = arbOwner.RemoveChainOwner(&auth, chainOwnerAddr2)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	isChainOwner, err = arbOwnerPublic.IsChainOwner(callOpts, chainOwnerAddr2)
	Require(t, err)
	if isChainOwner {
		Fatal(t, "expected owner2 to not be a chain owner")
	}

	_, err = arbOwnerPublic.RectifyChainOwner(&auth, chainOwnerAddr)
	if (err == nil) || (err.Error() != "execution reverted") {
		Fatal(t, "expected rectify chain owner to revert since it is already an owner")
	}
}

func TestArbOwnerDoesntRevert(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	auth := builder.L2Info.GetDefaultTransactOpts("Owner", ctx)

	arbOwner, err := precompilesgen.NewArbOwner(types.ArbOwnerAddress, builder.L2.Client)
	Require(t, err)

	chainConfig := params.ArbitrumDevTestChainConfig()
	chainConfig.ArbitrumChainParams.MaxCodeSize = 100
	serializedChainConfig, err := json.Marshal(chainConfig)
	Require(t, err)
	tx, err := arbOwner.SetChainConfig(&auth, string(serializedChainConfig))
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)

	tx, err = arbOwner.SetAmortizedCostCapBips(&auth, 77734)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)

	tx, err = arbOwner.ReleaseL1PricerSurplusFunds(&auth, big.NewInt(1))
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)

	tx, err = arbOwner.SetL2BaseFee(&auth, big.NewInt(1))
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
}

func TestArbGasInfoDoesntRevert(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	callOpts := &bind.CallOpts{Context: ctx}
	addr := common.BytesToAddress(crypto.Keccak256([]byte{})[:20])

	arbGasInfo, err := precompilesgen.NewArbGasInfo(types.ArbGasInfoAddress, builder.L2.Client)
	Require(t, err)

	_, err = arbGasInfo.GetGasBacklog(callOpts)
	Require(t, err)

	_, err = arbGasInfo.GetLastL1PricingUpdateTime(callOpts)
	Require(t, err)

	_, err = arbGasInfo.GetL1PricingFundsDueForRewards(callOpts)
	Require(t, err)

	_, err = arbGasInfo.GetL1PricingUnitsSinceUpdate(callOpts)
	Require(t, err)

	_, err = arbGasInfo.GetLastL1PricingSurplus(callOpts)
	Require(t, err)

	_, _, _, err = arbGasInfo.GetPricesInArbGas(callOpts)
	Require(t, err)

	_, _, _, err = arbGasInfo.GetPricesInArbGasWithAggregator(callOpts, addr)
	Require(t, err)

	_, err = arbGasInfo.GetAmortizedCostCapBips(callOpts)
	Require(t, err)

	_, err = arbGasInfo.GetL1FeesAvailable(callOpts)
	Require(t, err)

	_, _, _, _, _, _, err = arbGasInfo.GetPricesInWeiWithAggregator(callOpts, addr)
	Require(t, err)
}

func TestArbAggregatorBatchPosters(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	auth := builder.L2Info.GetDefaultTransactOpts("Owner", ctx)
	callOpts := &bind.CallOpts{Context: ctx}

	arbAggregator, err := precompilesgen.NewArbAggregator(types.ArbAggregatorAddress, builder.L2.Client)
	Require(t, err)

	arbDebug, err := precompilesgen.NewArbDebug(types.ArbDebugAddress, builder.L2.Client)
	Require(t, err)

	addr := common.BytesToAddress(crypto.Keccak256([]byte{})[:20])

	// initially should have one batch poster
	bps, err := arbAggregator.GetBatchPosters(callOpts)
	Require(t, err)
	if len(bps) != 1 {
		Fatal(t, "expected one batch poster")
	}

	// add addr as a batch poster
	tx, err := arbDebug.BecomeChainOwner(&auth)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	tx, err = arbAggregator.AddBatchPoster(&auth, addr)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)

	// there should now be two batch posters, and addr should be one of them
	bps, err = arbAggregator.GetBatchPosters(callOpts)
	Require(t, err)
	if len(bps) != 2 {
		Fatal(t, "expected two batch posters")
	}
	if bps[0] != addr && bps[1] != addr {
		Fatal(t, "expected addr to be a batch poster")
	}
}

func TestArbAggregatorGetPreferredAggregator(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	callOpts := &bind.CallOpts{Context: ctx}

	arbAggregator, err := precompilesgen.NewArbAggregator(types.ArbAggregatorAddress, builder.L2.Client)
	Require(t, err)

	addr := common.BytesToAddress(crypto.Keccak256([]byte{})[:20])

	prefAgg, isDefault, err := arbAggregator.GetPreferredAggregator(callOpts, addr)
	Require(t, err)
	if !isDefault {
		Fatal(t, "expected default preferred aggregator")
	}
	if prefAgg != l1pricing.BatchPosterAddress {
		Fatal(t, "expected default preferred aggregator to be", l1pricing.BatchPosterAddress, "got", prefAgg)
	}

	prefAgg, err = arbAggregator.GetDefaultAggregator(callOpts)
	Require(t, err)
	if prefAgg != l1pricing.BatchPosterAddress {
		Fatal(t, "expected default preferred aggregator to be", l1pricing.BatchPosterAddress, "got", prefAgg)
	}
}

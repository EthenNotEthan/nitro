// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package server_arb

import (
	"context"
	"fmt"

	"github.com/offchainlabs/nitro/util/containers"
	"github.com/offchainlabs/nitro/util/stopwaiter"
	"github.com/offchainlabs/nitro/validator"
)

type executionRun struct {
	stopwaiter.StopWaiter
	cache *MachineCache
}

func consumeMachine(promise *containers.Promise[validator.MachineStepResult], reqPosition uint64, machine MachineInterface, err error) {
	if err != nil {
		promise.ProduceError(err)
		return
	}
	machineStep := machine.GetStepCount()
	if reqPosition != machineStep {
		machineRunning := machine.IsRunning()
		if (machineRunning && reqPosition != machineStep) || machineStep > reqPosition {
			promise.ProduceError(fmt.Errorf("machine is in wrong position want: %d, got: %d", reqPosition, machine.GetStepCount()))
			return
		}

	}
	result := validator.MachineStepResult{
		Position:    machineStep,
		Status:      validator.MachineStatus(machine.Status()),
		GlobalState: machine.GetGlobalState(),
		Hash:        machine.Hash(),
	}
	promise.Produce(result)
}

// NewExecutionChallengeBackend creates a backend with the given arguments.
// Note: machineCache may be nil, but if present, it must not have a restricted range.
func NewExecutionRun(
	ctxIn context.Context,
	initialMachineGetter func(context.Context) (MachineInterface, error),
	config *MachineCacheConfig,
) (*executionRun, error) {
	exec := &executionRun{}
	exec.Start(ctxIn, exec)
	exec.cache = NewMachineCache(exec.GetContext(), initialMachineGetter, config)
	return exec, nil
}

func (e *executionRun) Close() {
	e.StopOnly()
}

func (e *executionRun) PrepareRange(start uint64, end uint64) {
	newCache := e.cache.SpawnCacheWithLimits(e.GetContext(), start, end)
	e.cache = newCache
}

func (e *executionRun) GetStepAt(position uint64) containers.PromiseInterface[validator.MachineStepResult] {
	promise := containers.NewPromise[validator.MachineStepResult]()
	cancel := e.LaunchThreadWithCancel(func(ctx context.Context) {
		var mach MachineInterface
		var err error
		if position == ^uint64(0) {
			mach, err = e.cache.GetFinalMachine(ctx)
		} else {
			// todo cache last machine
			mach, err = e.cache.GetMachineAt(ctx, position)
		}
		consumeMachine(&promise, position, mach, err)
	})
	promise.SetCancel(cancel)
	return &promise
}

type asyncProof struct {
	containers.Promise[[]byte]
	cancel func()
}

func (p *asyncProof) Close() {}

func (e *executionRun) GetProofAt(position uint64) containers.PromiseInterface[[]byte] {
	proof := &asyncProof{
		Promise: containers.NewPromise[[]byte](),
	}
	cancel := e.LaunchThreadWithCancel(func(ctx context.Context) {
		machine, err := e.cache.GetMachineAt(ctx, position)
		if err != nil {
			proof.ProduceError(err)
			return
		}
		proof.Produce(machine.ProveNextStep())
	})
	proof.cancel = cancel
	return proof
}

func (e *executionRun) GetLastStep() containers.PromiseInterface[validator.MachineStepResult] {
	return e.GetStepAt(^uint64(0))
}

// Copyright (c) 2025 Pano Operations Ltd
//
// Use of this software is governed by the Business Source License included
// in the LICENSE file and at panoptisDev.com/bsl11.
//
// Change Date: 2028-4-16
//
// On the date above, in accordance with the Business Source License, use of
// this software will be governed by the GNU Lesser General Public License v3.

package geth_adapter

import (
	"testing"

	"github.com/panoptisDev/tosca/go/tosca"
	common "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestStateDB_implementsVmStateDBInterface(t *testing.T) {
	var _ vm.StateDB = &StateDB{}
}

func TestStateDB_RefundSnapshots_RecoversProperRefund(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	context := tosca.NewMockTransactionContext(ctrl)

	context.EXPECT().CreateSnapshot().Return(tosca.Snapshot(12)).AnyTimes()
	context.EXPECT().RestoreSnapshot(tosca.Snapshot(12)).AnyTimes()

	db := StateDB{context: context}

	require.Equal(uint64(0), db.GetRefund())
	s1 := db.Snapshot()

	db.AddRefund(10)
	require.Equal(uint64(10), db.GetRefund())
	s2 := db.Snapshot()

	db.SubRefund(3)
	require.Equal(uint64(7), db.GetRefund())
	s3 := db.Snapshot()

	db.AddRefund(5)
	require.Equal(uint64(12), db.GetRefund())

	db.RevertToSnapshot(s3)
	require.Equal(uint64(7), db.GetRefund())

	db.RevertToSnapshot(s2)
	require.Equal(uint64(10), db.GetRefund())

	db.RevertToSnapshot(s1)
	require.Equal(uint64(0), db.GetRefund())
}

func TestStateDB_RevertToSnapshot_InvalidSnapshot_IsIgnored(t *testing.T) {
	tests := map[string]int{
		"negative": -1,
		"invalid":  0,
	}

	for name, snapshot := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			context := tosca.NewMockTransactionContext(ctrl)

			db := StateDB{context: context}
			db.RevertToSnapshot(snapshot)
		})
	}
}

func TestStateDB_GetStateAndCommittedStateReturnsOriginalAndCurrentState(t *testing.T) {
	ctrl := gomock.NewController(t)
	context := tosca.NewMockTransactionContext(ctrl)

	address := tosca.Address{0x1}
	key := tosca.Key{0x2}
	original := tosca.Word{0x3}
	current := tosca.Word{0x4}

	context.EXPECT().GetStorage(address, key).Return(original)
	context.EXPECT().GetCommittedStorage(address, key).Return(current)
	stateDB := NewStateDB(context)

	state, committed := stateDB.GetStateAndCommittedState(common.Address(address), common.Hash(key))
	require.Equal(t, original, tosca.Word(state))
	require.Equal(t, current, tosca.Word(committed))
}

// Copyright (c) 2025 Pano Operations Ltd
//
// Use of this software is governed by the Business Source License included
// in the LICENSE file and at panoptisDev.com/bsl11.
//
// Change Date: 2028-4-16
//
// On the date above, in accordance with the Business Source License, use of
// this software will be governed by the GNU Lesser General Public License v3.

package gen

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	. "github.com/panoptisDev/tosca/go/ct/common"
	"github.com/panoptisDev/tosca/go/ct/st"
	"github.com/panoptisDev/tosca/go/tosca"
	"golang.org/x/exp/maps"
	"pgregory.net/rand"
)

type AccountsGenerator struct {
	emptyAccounts        []emptyConstraint
	minBalance           []balanceConstraint
	maxBalance           []balanceConstraint
	warmCold             []warmColdConstraint
	delegationDesignator []delegationDesignatorConstraint
}

func NewAccountGenerator() *AccountsGenerator {
	return &AccountsGenerator{}
}

func (g *AccountsGenerator) Clone() *AccountsGenerator {
	return &AccountsGenerator{
		emptyAccounts:        slices.Clone(g.emptyAccounts),
		warmCold:             slices.Clone(g.warmCold),
		minBalance:           slices.Clone(g.minBalance),
		maxBalance:           slices.Clone(g.maxBalance),
		delegationDesignator: slices.Clone(g.delegationDesignator),
	}
}

func (g *AccountsGenerator) Restore(other *AccountsGenerator) {
	if g == other {
		return
	}
	g.emptyAccounts = slices.Clone(other.emptyAccounts)
	g.warmCold = slices.Clone(other.warmCold)
	g.minBalance = slices.Clone(other.minBalance)
	g.maxBalance = slices.Clone(other.maxBalance)
	g.delegationDesignator = slices.Clone(other.delegationDesignator)
}

func (g *AccountsGenerator) BindToAddressOfEmptyAccount(address Variable) {
	c := emptyConstraint{address, true}
	if !slices.Contains(g.emptyAccounts, c) {
		g.emptyAccounts = append(g.emptyAccounts, c)
	}
}

func (g *AccountsGenerator) BindToAddressOfNonEmptyAccount(address Variable) {
	c := emptyConstraint{address, false}
	if !slices.Contains(g.emptyAccounts, c) {
		g.emptyAccounts = append(g.emptyAccounts, c)
	}
}

func (g *AccountsGenerator) AddBalanceLowerBound(address Variable, value U256) {
	c := balanceConstraint{address, value}
	if !slices.Contains(g.minBalance, c) {
		g.minBalance = append(g.minBalance, c)
	}
}

func (g *AccountsGenerator) AddBalanceUpperBound(address Variable, value U256) {
	c := balanceConstraint{address, value}
	if !slices.Contains(g.maxBalance, c) {
		g.maxBalance = append(g.maxBalance, c)
	}
}

func (g *AccountsGenerator) BindWarm(address Variable) {
	v := warmColdConstraint{address, true}
	if !slices.Contains(g.warmCold, v) {
		g.warmCold = append(g.warmCold, v)
	}
}

func (g *AccountsGenerator) BindCold(address Variable) {
	v := warmColdConstraint{address, false}
	if !slices.Contains(g.warmCold, v) {
		g.warmCold = append(g.warmCold, v)
	}
}

func (g *AccountsGenerator) BindToAddressOfDelegatingAccount(address Variable, delegateAccountStatus tosca.AccessStatus) {
	v := delegationDesignatorConstraint{address, true, delegateAccountStatus}
	if !slices.Contains(g.delegationDesignator, v) {
		g.delegationDesignator = append(g.delegationDesignator, v)
	}
}

func (g *AccountsGenerator) BindToAddressOfNotDelegatingAccount(address Variable) {
	v := delegationDesignatorConstraint{address: address, isDelegated: false}
	if !slices.Contains(g.delegationDesignator, v) {
		g.delegationDesignator = append(g.delegationDesignator, v)
	}
}

func (g *AccountsGenerator) String() string {
	var parts []string

	sort.Slice(g.warmCold, func(i, j int) bool { return g.warmCold[i].Less(&g.warmCold[j]) })
	for _, con := range g.warmCold {
		if con.warm {
			parts = append(parts, fmt.Sprintf("warm(%v)", con.key))
		} else {
			parts = append(parts, fmt.Sprintf("cold(%v)", con.key))
		}
	}

	sort.Slice(g.emptyAccounts, func(i, j int) bool {
		return g.emptyAccounts[i].Less(&g.emptyAccounts[j])
	})
	for _, con := range g.emptyAccounts {
		if con.empty {
			parts = append(parts, fmt.Sprintf("empty(%v)", con.address))
		} else {
			parts = append(parts, fmt.Sprintf("!empty(%v)", con.address))
		}
	}

	sort.Slice(g.minBalance, func(i, j int) bool {
		return g.minBalance[i].Less(&g.minBalance[j])
	})
	for _, con := range g.minBalance {
		parts = append(parts, fmt.Sprintf("balance(%v) ≥ %v", con.address, con.value.DecimalString()))
	}

	sort.Slice(g.maxBalance, func(i, j int) bool {
		return g.maxBalance[i].Less(&g.maxBalance[j])
	})
	for _, con := range g.maxBalance {
		parts = append(parts, fmt.Sprintf("balance(%v) ≤ %v", con.address, con.value.DecimalString()))
	}

	sort.Slice(g.delegationDesignator, func(i, j int) bool {
		return g.delegationDesignator[i].Less(&g.delegationDesignator[j])
	})
	for _, con := range g.delegationDesignator {
		parts = append(parts, con.String())
	}

	return "{" + strings.Join(parts, ",") + "}"
}

func (g *AccountsGenerator) Generate(assignment Assignment, rnd *rand.Rand, accountAddress tosca.Address) (*st.Accounts, error) {

	// This closure checks whenever to variables
	// are the same variable or are already bound to the same value
	areVariablesClashing := func(a, b Variable) bool {
		if a == b {
			return true
		}
		if aValue, isBoundA := assignment[a]; isBoundA {
			if bValue, isBoundB := assignment[b]; isBoundB {
				return aValue == bValue
			}
		}
		return false
	}

	// Check for conflicts among empty constraints.
	sort.Slice(g.emptyAccounts, func(i, j int) bool {
		return g.emptyAccounts[i].Less(&g.emptyAccounts[j])
	})
	for i := 0; i < len(g.emptyAccounts)-1; i++ {
		a, b := g.emptyAccounts[i], g.emptyAccounts[i+1]
		if areVariablesClashing(a.address, b.address) && a.empty != b.empty {
			return nil, fmt.Errorf("%w, address %v conflicting empty constraints", ErrUnsatisfiable, a.address)
		}
	}

	// Check for conflicts among delegation designator constraints.
	sort.Slice(g.delegationDesignator, func(i, j int) bool {
		return g.delegationDesignator[i].Less(&g.delegationDesignator[j])
	})
	for i := 0; i < len(g.delegationDesignator)-1; i++ {
		a, b := g.delegationDesignator[i], g.delegationDesignator[i+1]
		// if a delegation designator constraint is conflicting with another one
		// by having the same target address but different access kind or enabled
		if areVariablesClashing(a.address, b.address) &&
			(a.isDelegated != b.isDelegated || a.delegateAccountStatus != b.delegateAccountStatus) {
			return nil, fmt.Errorf("%w, address %v conflicting delegation designator constraints", ErrUnsatisfiable, a.address)
		}
	}

	// Check for conflicts among empty constraints and delegation designator constraints.
	for _, ddCon := range g.delegationDesignator {
		if slices.ContainsFunc(g.emptyAccounts, func(c emptyConstraint) bool {
			return areVariablesClashing(c.address, ddCon.address) && c.empty
		}) {
			return nil, fmt.Errorf("%w, address %v conflicting delegation designator - empty constraints", ErrUnsatisfiable, ddCon.address)
		}
	}

	// Check for conflicts among warm/cold constraints.
	sort.Slice(g.warmCold, func(i, j int) bool { return g.warmCold[i].Less(&g.warmCold[j]) })
	for i := 0; i < len(g.warmCold)-1; i++ {
		a, b := g.warmCold[i], g.warmCold[i+1]
		if areVariablesClashing(a.key, b.key) && a.warm != b.warm {
			return nil, fmt.Errorf("%w, address %v conflicting warm/cold constraints", ErrUnsatisfiable, a.key)
		}
	}

	// When handling unbound variables, we need to generate an unused address for
	// them. We therefore track which addresses have already been used.
	addressesInUse := map[tosca.Address]bool{}
	addressesInUse[accountAddress] = true
	for _, con := range g.warmCold {
		if pos, isBound := assignment[con.key]; isBound {
			addressesInUse[NewAddress(pos)] = true
		}
	}
	getUnusedAddress := func() tosca.Address {
		for {
			address := RandomAddress(rnd)

			if _, isPresent := addressesInUse[address]; !isPresent {
				addressesInUse[address] = true
				return address
			}
		}
	}

	getBoundOrBindNewAddress := func(v Variable) tosca.Address {
		value, isBound := assignment[v]
		address := NewAddress(value)
		if !isBound {
			address = getUnusedAddress()
			assignment[v] = AddressToU256(address)
		}

		return address
	}

	accountsBuilder := st.NewAccountsBuilder()

	// Process empty constraints.
	emptyAccounts := map[tosca.Address]struct{}{}
	nonEmptyAccounts := map[tosca.Address]struct{}{}
	for _, con := range g.emptyAccounts {
		address := getBoundOrBindNewAddress(con.address)
		if con.empty {
			if _, found := nonEmptyAccounts[address]; found {
				return nil, fmt.Errorf("%w, address %v conflicting empty constraints", ErrUnsatisfiable, address)
			}
			emptyAccounts[address] = struct{}{}
		} else {
			if _, found := emptyAccounts[address]; found {
				return nil, fmt.Errorf("%w, address %v conflicting empty constraints", ErrUnsatisfiable, address)
			}
			nonEmptyAccounts[address] = struct{}{}
		}
	}
	for address := range emptyAccounts {
		accountsBuilder.SetBalance(address, NewU256(0))
		accountsBuilder.SetCode(address, Bytes{})
	}
	for address := range nonEmptyAccounts {
		switch rand.Intn(3) {
		case 0:
			accountsBuilder.SetBalance(address, NewU256(1))
		case 1:
			accountsBuilder.SetCode(address, NewBytes([]byte{1}))
		case 2:
			accountsBuilder.SetBalance(address, NewU256(1))
			accountsBuilder.SetCode(address, NewBytes([]byte{1}))
		}
	}

	// Process warm/cold constraints.
	for _, con := range g.warmCold {
		address := getBoundOrBindNewAddress(con.key)
		if con.warm {
			accountsBuilder.SetWarm(address)
		}
	}

	// Apply balance constraints.
	if err := g.processBalanceConstraints(
		getBoundOrBindNewAddress,
		emptyAccounts,
		rnd,
		accountsBuilder,
	); err != nil {
		return nil, err
	}

	// Add delegation designator constraints.
	// see: https://eips.ethereum.org/EIPS/eip-7702
	for _, con := range g.delegationDesignator {
		calleeAddress := getBoundOrBindNewAddress(con.address)
		if con.isDelegated {
			addr := getUnusedAddress()
			accountsBuilder.SetCode(calleeAddress, NewDelegationDesignator(addr))
			if con.delegateAccountStatus == tosca.WarmAccess {
				accountsBuilder.SetWarm(addr)
			}
		} else {
			// any code segment not being a delegation designator
			accountsBuilder.SetCode(calleeAddress, NewBytes([]byte{0xab, 0xcd}))
		}
	}

	// Some random entries.
	for i, max := 0, rnd.Intn(5); i < max; i++ {
		address := getUnusedAddress()
		accountsBuilder.SetBalance(address, RandU256(rnd))
		if rnd.Intn(2) == 1 {
			accountsBuilder.SetCode(address, RandomBytesOfSize(rnd, 42))
		}
		if rnd.Intn(2) == 1 {
			accountsBuilder.SetWarm(address)
		}
	}

	// Add balance and code for the account executing the code.
	if !accountsBuilder.Exists(accountAddress) {
		accountsBuilder.SetBalance(accountAddress, RandU256(rnd))
	}
	accountsBuilder.SetCode(accountAddress, RandomBytesOfSize(rnd, 42))

	return accountsBuilder.Build(), nil
}

func (g *AccountsGenerator) processBalanceConstraints(
	getBoundOrBindNewAddress func(v Variable) tosca.Address,
	mustBeEmptyAccounts map[tosca.Address]struct{},
	rnd *rand.Rand,
	result *st.AccountsBuilder,
) error {

	// Convert variable based constraints into lower and upper bounds for
	// concrete addresses by fetching variable bindings or assigning new addresses.
	lowerBounds := map[tosca.Address]U256{}
	for _, con := range g.minBalance {
		address := getBoundOrBindNewAddress(con.address)
		current := lowerBounds[address]
		if current.Lt(con.value) {
			lowerBounds[address] = con.value
		}
	}

	upperBounds := map[tosca.Address]U256{}
	for _, con := range g.maxBalance {
		address := getBoundOrBindNewAddress(con.address)
		current, found := upperBounds[address]
		if !found || current.Gt(con.value) {
			upperBounds[address] = con.value
		}
	}
	for address := range mustBeEmptyAccounts {
		upperBounds[address] = NewU256(0)
	}

	// Get list of all accounts to be resolved.
	accounts := maps.Keys(lowerBounds)
	for _, address := range maps.Keys(upperBounds) {
		if _, isPresent := lowerBounds[address]; !isPresent {
			accounts = append(accounts, address)
		}
	}

	// Apply constraints to the accounts to obtain balances.
	for _, address := range accounts {
		lower, hasLower := lowerBounds[address]
		upper, hasUpper := upperBounds[address]

		if !hasLower {
			lower = NewU256()
		}
		if !hasUpper {
			upper = MaxU256()
		}

		if lower.Gt(upper) {
			return fmt.Errorf("%w, conflicting balance constraints", ErrUnsatisfiable)
		}

		result.SetBalance(address, RandU256Between(rnd, lower, upper))
	}

	return nil
}

type emptyConstraint struct {
	address Variable
	empty   bool
}

func (a *emptyConstraint) Less(b *emptyConstraint) bool {
	if a.address != b.address {
		return a.address < b.address
	}
	return a.empty != b.empty && a.empty
}

type balanceConstraint struct {
	address Variable
	value   U256
}

func (a *balanceConstraint) Less(b *balanceConstraint) bool {
	if a.address != b.address {
		return a.address < b.address
	}
	return a.value.Lt(b.value)
}

type delegationDesignatorConstraint struct {
	address               Variable
	isDelegated           bool
	delegateAccountStatus tosca.AccessStatus
}

func (a *delegationDesignatorConstraint) Less(b *delegationDesignatorConstraint) bool {
	if a.address != b.address {
		return a.address < b.address
	}
	if a.isDelegated != b.isDelegated {
		return !a.isDelegated && b.isDelegated
	}
	return bool(!a.delegateAccountStatus && b.delegateAccountStatus)
}

func (a *delegationDesignatorConstraint) String() string {
	if !a.isDelegated {
		return fmt.Sprintf("!isDelegated(%v)", a.address)
	}
	if a.delegateAccountStatus == tosca.WarmAccess {
		return fmt.Sprintf("isDelegated(%v),warm(delegateOf(%v))", a.address, a.address)
	}
	return fmt.Sprintf("isDelegated(%v),cold(delegateOf(%v))", a.address, a.address)
}

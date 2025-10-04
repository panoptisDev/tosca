// Copyright (c) 2025 Pano Operations Ltd
//
// Use of this software is governed by the Business Source License included
// in the LICENSE file and at panoptisDev.com/bsl11.
//
// Change Date: 2028-4-16
//
// On the date above, in accordance with the Business Source License, use of
// this software will be governed by the GNU Lesser General Public License v3.

package interpreter_test

import (
	"fmt"
	"slices"
	"strings"

	_ "github.com/panoptisDev/tosca/go/interpreter/evmone"
	_ "github.com/panoptisDev/tosca/go/interpreter/evmrs"
	_ "github.com/panoptisDev/tosca/go/interpreter/evmzero"
	_ "github.com/panoptisDev/tosca/go/interpreter/geth"
	"github.com/panoptisDev/tosca/go/interpreter/lfvm"
	"github.com/panoptisDev/tosca/go/tosca"
	"golang.org/x/exp/maps"
)

func init() {
	// Experimental LFVM configurations should be covered by integration tests
	// as they might be used by down-stream tools and for debugging.
	err := lfvm.RegisterExperimentalInterpreterConfigurations()
	if err != nil {
		panic(fmt.Errorf("failed to register experimental LFVM configurations: %v", err))
	}
}

// getAllInterpreterVariantsForTests returns all registered interpreter variants
// that should be covered in integration tests.
func getAllInterpreterVariantsForTests() []string {
	// TODO: re-add logging variants once logging is no longer writing everything to stdout
	return slices.DeleteFunc(
		maps.Keys(tosca.GetAllRegisteredInterpreters()),
		func(s string) bool { return strings.Contains(s, "logging") },
	)
}

// skipTestForVariant returns true, if the given test should be skipped for
// the given variant.
func skipTestForVariant(testName string, variant string) bool {
	disabledTest := map[string][]string{
		"TestNoReturnDataForCreate": {
			"evmone", "evmone-basic", "evmone-advanced",
		},
	}
	return slices.Contains(disabledTest[testName], variant)
}

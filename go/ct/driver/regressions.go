// Copyright (c) 2025 Pano Operations Ltd
//
// Use of this software is governed by the Business Source License included
// in the LICENSE file and at panoptisDev.com/bsl11.
//
// Change Date: 2028-4-16
//
// On the date above, in accordance with the Business Source License, use of
// this software will be governed by the GNU Lesser General Public License v3.

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	cliUtils "github.com/panoptisDev/tosca/go/ct/driver/cli"
	"github.com/panoptisDev/tosca/go/ct/spc"
	"github.com/panoptisDev/tosca/go/ct/st"
	"github.com/urfave/cli/v2"
	"golang.org/x/exp/maps"
)

var RegressionsCmd = cliUtils.AddCommonFlags(cli.Command{
	Action:    doRegressionTests,
	Name:      "regressions",
	Usage:     "Run Conformance Tests on regression test inputs on an EVM implementation",
	ArgsUsage: "<EVM>",
	Flags: []cli.Flag{
		&cli.StringSliceFlag{
			Name:  "input",
			Usage: "run given input file, or all files in the given directory (recursively)",
			Value: cli.NewStringSlice("./regression_inputs"),
		},
	},
})

func enumerateInputs(inputs []string) ([]string, error) {
	var inputFiles []string

	for _, input := range inputs {
		path, err := filepath.Abs(input)
		if err != nil {
			return nil, err
		}

		stat, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if !stat.IsDir() {
			inputFiles = append(inputFiles, path)
			continue
		}

		entries, err := os.ReadDir(input)
		if err != nil {
			return nil, err
		}

		for _, entry := range entries {
			filePath := filepath.Join(path, entry.Name())
			if entry.IsDir() {
				recInputs, err := enumerateInputs([]string{filePath})
				if err != nil {
					return nil, err
				}
				inputFiles = append(inputFiles, recInputs...)
			} else {
				inputFiles = append(inputFiles, filePath)
			}
		}
	}

	return inputFiles, nil
}

func doRegressionTests(context *cli.Context) error {
	var evmIdentifier string
	if context.Args().Len() >= 1 {
		evmIdentifier = context.Args().Get(0)
	}

	evm, ok := evms[evmIdentifier]
	if !ok {
		return fmt.Errorf("invalid EVM identifier, use one of: %v", maps.Keys(evms))
	}

	inputs := context.StringSlice("input")
	inputs, err := enumerateInputs(inputs)
	if err != nil {
		return err
	}

	var issues []error
	for _, input := range inputs {
		fmt.Printf("Running regression tests for %v\n", input)
		state, err := st.ImportStateJSON(input)
		if err != nil {
			issues = append(issues, fmt.Errorf("failed to import state from %v: %w", input, err))
			continue
		}

		rules := spc.Spec.GetRulesFor(state)

		if len(rules) == 0 {
			issues = append(issues, fmt.Errorf("no rules apply for input %v", input))
			continue
		}

		evaluationCount := 0

		for _, rule := range rules {
			input := state.Clone()
			expected := state.Clone()
			rule.Effect.Apply(expected)

			// TODO: do not only skip state but change 'pc_on_data_is_ignored' rule to anyEffect, see #954
			// Pc on data is not supported
			if !state.Code.IsCode(int(state.Pc)) {
				continue
			}

			result, err := evm.StepN(input.Clone(), 1)
			if err != nil {
				issues = append(issues, fmt.Errorf("failed to evaluate rule %v, %w", rule.Name, err))
				continue
			}

			if !result.Eq(expected) {
				issues = append(issues, fmt.Errorf("unexpected result for rule %v, diff %v", rule.Name, formatDiffForUser(input, result, expected, rule.Name)))
				continue
			}

			evaluationCount++
		}
		fmt.Printf("OK: (rules evaluated: %d)\n", evaluationCount)
	}

	return errors.Join(issues...)
}

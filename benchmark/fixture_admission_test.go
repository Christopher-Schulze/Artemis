package benchmark

import (
	"strings"
	"testing"
)

func TestArtemisRunnerRetainsActualFixtureInitializationFailure(t *testing.T) {
	cases := []struct {
		name      string
		scenarios []Scenario
	}{
		{"empty corpus", []Scenario{}},
		{"excess entry count", make([]Scenario, 1025)},
		{"duplicate actual route", []Scenario{{ID: "same", HTML: "first"}, {ID: "same", HTML: "second"}}},
		{"invalid route", []Scenario{{ID: "{invalid}", HTML: "body"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := NewArtemisRunner(tc.scenarios, false)
			defer func() {
				if err := runner.Close(); err != nil {
					t.Error(err)
				}
			}()
			if runner.initializationErr == nil {
				t.Fatal("fixture rejection lost its actual cause")
			}
			scenario := Scenario{}
			if len(tc.scenarios) > 0 {
				scenario = tc.scenarios[0]
			}
			result := runner.RunScenario(t.Context(), scenario)
			if result.OK || result.Validated || !strings.Contains(result.Error, "register benchmark fixture") {
				t.Fatalf("unregistered fixture became valid sample: %+v", result)
			}
			if err := runner.Reset(false); err == nil {
				t.Fatal("failed fixture owner became resettable valid runner")
			}
		})
	}
}

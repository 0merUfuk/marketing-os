package domain

import (
	"math"
	"testing"
)

func TestWorkflowRunStateTransitionsAreMonotonicAndSafe(t *testing.T) {
	t.Parallel()
	allowed := [][2]RunStatus{
		{RunPending, RunRunning}, {RunPending, RunBlocked}, {RunPending, RunKilled},
		{RunRunning, RunNoAction}, {RunRunning, RunAwaitingApproval}, {RunRunning, RunFailed},
		{RunRunning, RunCancelled}, {RunRunning, RunKilled},
	}
	for _, pair := range allowed {
		if !CanTransition(pair[0], pair[1]) {
			t.Errorf("transition %s -> %s should be allowed", pair[0], pair[1])
		}
	}
	for _, pair := range [][2]RunStatus{
		{RunAwaitingApproval, RunCompleted}, {RunFailed, RunRunning}, {RunNoAction, RunRunning}, {RunRunning, RunCompleted},
	} {
		if CanTransition(pair[0], pair[1]) {
			t.Errorf("unsafe transition %s -> %s was allowed", pair[0], pair[1])
		}
	}
}

func TestWorkflowDefinitionRequiresSafetyAndStoppingFields(t *testing.T) {
	t.Parallel()
	definition := ReleaseToMarketingDefinition("alpha")
	if err := definition.Validate(); err != nil {
		t.Fatalf("valid release workflow rejected: %v", err)
	}
	definition.SelfCheck = ""
	if err := definition.Validate(); err == nil {
		t.Fatal("workflow without self-check was accepted")
	}
	definition = ReleaseToMarketingDefinition("alpha")
	definition.MaxCostUSD = math.NaN()
	if err := definition.Validate(); err == nil {
		t.Fatal("workflow with a non-finite cost limit was accepted")
	}
}

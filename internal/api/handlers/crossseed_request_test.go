package handlers

import (
	"encoding/json"
	"testing"
)

func TestAutomationSettingsRequest_UnmarshalDetectsPreventReaddFlag(t *testing.T) {
	t.Run("absent field", func(t *testing.T) {
		var req automationSettingsRequest
		payload := []byte(`{"enabled": true}`)
		if err := json.Unmarshal(payload, &req); err != nil {
			t.Fatalf("unexpected error decoding request: %v", err)
		}
		if req.preventReaddProvided {
			t.Fatalf("expected preventReadd flag to be marked absent")
		}
	})

	t.Run("present field", func(t *testing.T) {
		var req automationSettingsRequest
		payload := []byte(`{"preventReaddPreviouslyAdded": false}`)
		if err := json.Unmarshal(payload, &req); err != nil {
			t.Fatalf("unexpected error decoding request: %v", err)
		}
		if !req.preventReaddProvided {
			t.Fatalf("expected preventReadd flag to be marked present")
		}
		if req.PreventReaddPreviouslyAdded {
			t.Fatalf("expected preventReadd value to remain false when provided")
		}
	})
}

package controller

import (
	"encoding/json"
)

type suspendedState struct {
	Replicas *int32 `json:"replicas,omitempty"`
	Suspend  *bool  `json:"suspend,omitempty"`
}

func encodeSuspendedState(state suspendedState) string {
	out, _ := json.Marshal(state)
	return string(out)
}

func decodeSuspendedState(encoded string) (suspendedState, error) {
	state := suspendedState{}
	err := json.Unmarshal([]byte(encoded), &state)
	return state, err
}

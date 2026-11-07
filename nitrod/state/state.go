package state

import "encoding/json"

type Snapshot struct {
	Features       []string                   `json:"features"`
	ProfileChoices []string                   `json:"platform_profile_choices,omitempty"`
	Values         map[string]json.RawMessage `json:"values"`
}

func NewSnapshot(features, choices []string) Snapshot {
	return Snapshot{
		Features:       append([]string(nil), features...),
		ProfileChoices: append([]string(nil), choices...),
		Values:         make(map[string]json.RawMessage),
	}
}

func (s *Snapshot) Set(key string, value json.RawMessage) {
	if s.Values == nil {
		s.Values = make(map[string]json.RawMessage)
	}
	s.Values[key] = append(json.RawMessage(nil), value...)
}

func (s Snapshot) Clone() Snapshot {
	copy := NewSnapshot(s.Features, s.ProfileChoices)
	for key, value := range s.Values {
		copy.Set(key, value)
	}
	return copy
}

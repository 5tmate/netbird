package server

import "testing"

func TestRegistrationFrameEnabled(t *testing.T) {
	cases := map[string]bool{"": false, "false": false, "0": false, "nonsense": false, "true": true, "1": true}
	for value, want := range cases {
		t.Setenv("NB_SIGNAL_REGISTRATION_FRAME", value)
		if got := registrationFrameEnabled(); got != want {
			t.Errorf("NB_SIGNAL_REGISTRATION_FRAME=%q: got %v, want %v", value, got, want)
		}
	}
}

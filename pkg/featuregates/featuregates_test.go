/*
Copyright 2023 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package featuregates

import (
	"os"
	"testing"
)

func TestEnabled(t *testing.T) {
	tests := []struct {
		name     string
		gate     string
		envValue string
		want     bool
	}{
		{
			name:     "feature gate enabled",
			gate:     CELWebhookNoGate,
			envValue: "true",
			want:     true,
		},
		{
			name:     "feature gate disabled",
			gate:     CELWebhookNoGate,
			envValue: "false",
			want:     false,
		},
		{
			name:     "feature gate not set",
			gate:     CELWebhookNoGate,
			envValue: "",
			want:     false,
		},
		{
			name:     "feature gate invalid value",
			gate:     CELWebhookNoGate,
			envValue: "invalid",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			if tt.envValue != "" {
				os.Setenv(tt.gate, tt.envValue)
				defer os.Unsetenv(tt.gate)
			} else {
				os.Unsetenv(tt.gate)
			}

			got := Enabled(tt.gate)
			if got != tt.want {
				t.Errorf("Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCELWebhookNoGateConstant(t *testing.T) {
	expected := "CEL_WEBHOOK_NO_GATE"
	if CELWebhookNoGate != expected {
		t.Errorf("CELWebhookNoGate = %q, want %q", CELWebhookNoGate, expected)
	}
}

// Made with Bob

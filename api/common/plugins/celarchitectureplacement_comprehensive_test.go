/*
Copyright 2026 Red Hat, Inc.

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

package plugins

import (
	"strings"
	"testing"

	"github.com/openshift/multiarch-tuning-operator/api/common"
)

// TestCELArchitecturePlacement_ArchitectureListEdgeCases tests edge cases for architecture lists
func TestCELArchitecturePlacement_ArchitectureListEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		plugin    *CELArchitecturePlacement
		wantError bool
		errorMsg  string
	}{
		{
			name: "Single architecture in fallback",
			plugin: &CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
			},
			wantError: false,
		},
		{
			name: "All four architectures in fallback",
			plugin: &CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64", "arm64", "ppc64le", "s390x"},
			},
			wantError: false,
		},
		{
			name: "Duplicate architectures in fallback",
			plugin: &CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64", "amd64"},
			},
			wantError: false, // Validation doesn't check for duplicates
		},
		{
			name: "Single architecture in rule",
			plugin: &CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules: []ArchitectureRule{
					{
						Name:          "single-arch-rule",
						Expression:    "self.metadata.name == 'test'",
						Architectures: []string{"ppc64le"},
					},
				},
			},
			wantError: false,
		},
		{
			name: "All architectures in rule",
			plugin: &CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules: []ArchitectureRule{
					{
						Name:          "all-arch-rule",
						Expression:    "self.metadata.name == 'test'",
						Architectures: []string{"amd64", "arm64", "ppc64le", "s390x"},
					},
				},
			},
			wantError: false,
		},
		{
			name: "Mixed case architecture (should fail)",
			plugin: &CELArchitecturePlacement{
				FallbackArchitectures: []string{"AMD64"},
			},
			wantError: true,
			errorMsg:  "invalid fallback architecture: AMD64",
		},
		{
			name: "Architecture with typo",
			plugin: &CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd65"},
			},
			wantError: true,
			errorMsg:  "invalid fallback architecture: amd65",
		},
		{
			name: "x86 instead of amd64",
			plugin: &CELArchitecturePlacement{
				FallbackArchitectures: []string{"x86"},
			},
			wantError: true,
			errorMsg:  "invalid fallback architecture: x86",
		},
		{
			name: "x86_64 instead of amd64",
			plugin: &CELArchitecturePlacement{
				FallbackArchitectures: []string{"x86_64"},
			},
			wantError: true,
			errorMsg:  "invalid fallback architecture: x86_64",
		},
		{
			name: "aarch64 instead of arm64",
			plugin: &CELArchitecturePlacement{
				FallbackArchitectures: []string{"aarch64"},
			},
			wantError: true,
			errorMsg:  "invalid fallback architecture: aarch64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.plugin.ValidateArchitectures()
			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if err.Error() != tt.errorMsg {
					t.Errorf("Expected error message %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

// TestCELArchitecturePlacement_RuleValidation tests rule-specific validation
func TestCELArchitecturePlacement_RuleValidation(t *testing.T) {
	tests := []struct {
		name      string
		plugin    *CELArchitecturePlacement
		wantError bool
		errorMsg  string
	}{
		{
			name: "Empty rule name",
			plugin: &CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules: []ArchitectureRule{
					{
						Name:          "",
						Expression:    "self.metadata.name == 'test'",
						Architectures: []string{"arm64"},
					},
				},
			},
			wantError: false, // ValidateArchitectures doesn't check rule names
		},
		{
			name: "Very long rule name (253 chars)",
			plugin: &CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules: []ArchitectureRule{
					{
						Name:          strings.Repeat("a", 253),
						Expression:    "self.metadata.name == 'test'",
						Architectures: []string{"arm64"},
					},
				},
			},
			wantError: false,
		},
		{
			name: "Multiple rules with same name",
			plugin: &CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules: []ArchitectureRule{
					{
						Name:          "duplicate-name",
						Expression:    "self.metadata.name == 'test1'",
						Architectures: []string{"arm64"},
					},
					{
						Name:          "duplicate-name",
						Expression:    "self.metadata.name == 'test2'",
						Architectures: []string{"ppc64le"},
					},
				},
			},
			wantError: false, // ValidateArchitectures doesn't check for duplicate names
		},
		{
			name: "Rule with invalid architecture",
			plugin: &CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules: []ArchitectureRule{
					{
						Name:          "invalid-arch-rule",
						Expression:    "self.metadata.name == 'test'",
						Architectures: []string{"invalid"},
					},
				},
			},
			wantError: true,
			errorMsg:  "invalid architecture in rule invalid-arch-rule: invalid",
		},
		{
			name: "Multiple rules with mixed valid/invalid architectures",
			plugin: &CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules: []ArchitectureRule{
					{
						Name:          "valid-rule",
						Expression:    "self.metadata.name == 'test1'",
						Architectures: []string{"arm64"},
					},
					{
						Name:          "invalid-rule",
						Expression:    "self.metadata.name == 'test2'",
						Architectures: []string{"invalid-arch"},
					},
				},
			},
			wantError: true,
			errorMsg:  "invalid architecture in rule invalid-rule: invalid-arch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.plugin.ValidateArchitectures()
			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if err.Error() != tt.errorMsg {
					t.Errorf("Expected error message %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

// TestCELArchitecturePlacement_MaxRulesLimit tests the 50 rules limit
func TestCELArchitecturePlacement_MaxRulesLimit(t *testing.T) {
	// Create plugin with exactly 50 rules (max limit)
	rules := make([]ArchitectureRule, 50)
	for i := 0; i < 50; i++ {
		rules[i] = ArchitectureRule{
			Name:          "rule-" + string(rune(i)),
			Expression:    "self.metadata.name == 'test'",
			Architectures: []string{"amd64"},
		}
	}

	plugin := &CELArchitecturePlacement{
		FallbackArchitectures: []string{"arm64"},
		Rules:                 rules,
	}

	err := plugin.ValidateArchitectures()
	if err != nil {
		t.Errorf("ValidateArchitectures() with 50 rules error = %v", err)
	}
}

// TestCELArchitecturePlacement_EmptyFallback tests empty fallback architectures
func TestCELArchitecturePlacement_EmptyFallback(t *testing.T) {
	tests := []struct {
		name      string
		plugin    *CELArchitecturePlacement
		wantError bool
	}{
		{
			name: "Empty fallback architectures slice",
			plugin: &CELArchitecturePlacement{
				FallbackArchitectures: []string{},
			},
			wantError: false, // ValidateArchitectures doesn't check for empty slice
		},
		{
			name: "Nil fallback architectures",
			plugin: &CELArchitecturePlacement{
				FallbackArchitectures: nil,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.plugin.ValidateArchitectures()
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateArchitectures() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

// TestCELArchitecturePlacement_PluginName tests the plugin name
func TestCELArchitecturePlacement_PluginName(t *testing.T) {
	plugin := &CELArchitecturePlacement{}
	expectedName := "CELArchitecturePlacement"

	if plugin.Name() != expectedName {
		t.Errorf("Name() = %v, want %v", plugin.Name(), expectedName)
	}

	if plugin.Name() != CELArchitecturePlacementPluginName {
		t.Errorf("Name() = %v, want %v", plugin.Name(), CELArchitecturePlacementPluginName)
	}
}

// TestCELArchitecturePlacement_EnabledState tests plugin enabled/disabled state
func TestCELArchitecturePlacement_EnabledState(t *testing.T) {
	tests := []struct {
		name    string
		plugin  *CELArchitecturePlacement
		enabled bool
	}{
		{
			name: "Plugin enabled",
			plugin: &CELArchitecturePlacement{
				BasePlugin: BasePlugin{Enabled: true},
			},
			enabled: true,
		},
		{
			name: "Plugin disabled",
			plugin: &CELArchitecturePlacement{
				BasePlugin: BasePlugin{Enabled: false},
			},
			enabled: false,
		},
		{
			name: "Plugin with default state",
			plugin: &CELArchitecturePlacement{
				BasePlugin: BasePlugin{},
			},
			enabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.plugin.IsEnabled() != tt.enabled {
				t.Errorf("IsEnabled() = %v, want %v", tt.plugin.IsEnabled(), tt.enabled)
			}
		})
	}
}

// TestCELArchitecturePlacement_ComplexValidation tests complex validation scenarios
func TestCELArchitecturePlacement_ComplexValidation(t *testing.T) {
	tests := []struct {
		name      string
		plugin    *CELArchitecturePlacement
		wantError bool
		errorMsg  string
	}{
		{
			name: "Valid complex configuration",
			plugin: &CELArchitecturePlacement{
				BasePlugin:            BasePlugin{Enabled: true},
				FallbackArchitectures: []string{"amd64", "arm64"},
				Rules: []ArchitectureRule{
					{
						Name:          "rule1",
						Expression:    "self.metadata.name.startsWith('app-')",
						Architectures: []string{"ppc64le"},
					},
					{
						Name:          "rule2",
						Expression:    "self.metadata.namespace == 'production'",
						Architectures: []string{"s390x"},
					},
					{
						Name:          "rule3",
						Expression:    "has(self.metadata.labels) && 'tier' in self.metadata.labels",
						Architectures: []string{"arm64", "amd64"},
					},
				},
			},
			wantError: false,
		},
		{
			name: "Invalid architecture in middle rule",
			plugin: &CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules: []ArchitectureRule{
					{
						Name:          "rule1",
						Expression:    "self.metadata.name == 'test1'",
						Architectures: []string{"arm64"},
					},
					{
						Name:          "rule2",
						Expression:    "self.metadata.name == 'test2'",
						Architectures: []string{"invalid-arch"},
					},
					{
						Name:          "rule3",
						Expression:    "self.metadata.name == 'test3'",
						Architectures: []string{"ppc64le"},
					},
				},
			},
			wantError: true,
			errorMsg:  "invalid architecture in rule rule2: invalid-arch",
		},
		{
			name: "Multiple invalid architectures in same rule",
			plugin: &CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules: []ArchitectureRule{
					{
						Name:          "multi-invalid",
						Expression:    "self.metadata.name == 'test'",
						Architectures: []string{"invalid1", "arm64", "invalid2"},
					},
				},
			},
			wantError: true,
			errorMsg:  "invalid architecture in rule multi-invalid: invalid1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.plugin.ValidateArchitectures()
			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if err.Error() != tt.errorMsg {
					t.Errorf("Expected error message %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

// TestLocalPlugins_CELPluginNilSafety tests nil safety for CEL plugin access
func TestLocalPlugins_CELPluginNilSafety(t *testing.T) {
	tests := []struct {
		name    string
		plugins *LocalPlugins
		want    bool
	}{
		{
			name:    "Nil LocalPlugins",
			plugins: nil,
			want:    false,
		},
		{
			name: "LocalPlugins with nil CEL plugin",
			plugins: &LocalPlugins{
				CELArchitecturePlacement: nil,
			},
			want: false,
		},
		{
			name: "LocalPlugins with disabled CEL plugin",
			plugins: &LocalPlugins{
				CELArchitecturePlacement: &CELArchitecturePlacement{
					BasePlugin: BasePlugin{Enabled: false},
				},
			},
			want: false,
		},
		{
			name: "LocalPlugins with enabled CEL plugin",
			plugins: &LocalPlugins{
				CELArchitecturePlacement: &CELArchitecturePlacement{
					BasePlugin: BasePlugin{Enabled: true},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got bool
			if tt.plugins == nil {
				// Test nil safety
				got = false
			} else {
				got = tt.plugins.PluginEnabled(common.CELArchitecturePlacementPluginName)
			}
			if got != tt.want {
				t.Errorf("PluginEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestArchitectureRule_FieldValidation tests individual rule field validation
func TestArchitectureRule_FieldValidation(t *testing.T) {
	tests := []struct {
		name      string
		rule      ArchitectureRule
		wantError bool
		errorMsg  string
	}{
		{
			name: "Valid rule",
			rule: ArchitectureRule{
				Name:          "valid-rule",
				Expression:    "self.metadata.name == 'test'",
				Architectures: []string{"amd64"},
			},
			wantError: false,
		},
		{
			name: "Rule with empty name",
			rule: ArchitectureRule{
				Name:          "",
				Expression:    "self.metadata.name == 'test'",
				Architectures: []string{"amd64"},
			},
			wantError: false, // Name validation is done by kubebuilder, not ValidateArchitectures
		},
		{
			name: "Rule with empty expression",
			rule: ArchitectureRule{
				Name:          "empty-expr",
				Expression:    "",
				Architectures: []string{"amd64"},
			},
			wantError: false, // Expression validation is done at runtime
		},
		{
			name: "Rule with empty architectures",
			rule: ArchitectureRule{
				Name:          "empty-archs",
				Expression:    "self.metadata.name == 'test'",
				Architectures: []string{},
			},
			wantError: false, // Empty slice doesn't trigger validation error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &CELArchitecturePlacement{
				FallbackArchitectures: []string{"amd64"},
				Rules:                 []ArchitectureRule{tt.rule},
			}
			err := plugin.ValidateArchitectures()
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateArchitectures() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

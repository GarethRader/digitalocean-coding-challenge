package server

import "testing"

func TestRuleMatches(t *testing.T) {
	type testCase struct {
		name       string
		rule       Rule
		attributes map[string]string
		expected   bool
	}

	cases := []testCase{
		{
			name:       "equals true",
			rule:       Rule{Attribute: "region", Operator: "equals", Value: "us-west", State: true},
			attributes: map[string]string{"region": "us-west"},
			expected:   true,
		},
		{
			name:       "equals false",
			rule:       Rule{Attribute: "region", Operator: "equals", Value: "us-west", State: true},
			attributes: map[string]string{"region": "eu-central"},
			expected:   false,
		},
		{
			name:       "not_equals true",
			rule:       Rule{Attribute: "subscription_tier", Operator: "not_equals", Value: "gold", State: true},
			attributes: map[string]string{"subscription_tier": "silver"},
			expected:   true,
		},
		{
			name:       "in with string slice",
			rule:       Rule{Attribute: "region", Operator: "in", Value: []string{"us-west", "eu-central"}, State: true},
			attributes: map[string]string{"region": "eu-central"},
			expected:   true,
		},
		{
			name:       "in with interface slice",
			rule:       Rule{Attribute: "region", Operator: "in", Value: []interface{}{"us-west", "eu-central"}, State: true},
			attributes: map[string]string{"region": "us-west"},
			expected:   true,
		},
		{
			name:       "not_in with comma-separated string",
			rule:       Rule{Attribute: "region", Operator: "not_in", Value: "us-west, eu-central", State: true},
			attributes: map[string]string{"region": "ap-southeast"},
			expected:   true,
		},
		{
			name:       "contains matches",
			rule:       Rule{Attribute: "department", Operator: "contains", Value: "market", State: true},
			attributes: map[string]string{"department": "marketing"},
			expected:   true,
		},
		{
			name:       "unknown operator",
			rule:       Rule{Attribute: "region", Operator: "unknown", Value: "x", State: true},
			attributes: map[string]string{"region": "x"},
			expected:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.rule.Matches(tc.attributes); got != tc.expected {
				t.Fatalf("expected %v for rule %v with attributes %v, got %v", tc.expected, tc.rule, tc.attributes, got)
			}
		})
	}
}

func TestFeatureFlagEvaluate(t *testing.T) {
	flag := FeatureFlag{
		Name:         "beta_feature",
		DefaultState: false,
		Rules: []Rule{
			{Attribute: "subscription_tier", Operator: "equals", Value: "gold", State: true},
			{Attribute: "region", Operator: "equals", Value: "us-west", State: true},
		},
	}

	enabled, reason := flag.Evaluate(UserContext{SubscriptionTier: "gold"})
	if !enabled || reason != "rule 1 matched" {
		t.Fatalf("expected first rule match for gold subscriber, got enabled=%v reason=%s", enabled, reason)
	}

	enabled, reason = flag.Evaluate(UserContext{Region: "us-west"})
	if !enabled || reason != "rule 2 matched" {
		t.Fatalf("expected second rule match for us-west region, got enabled=%v reason=%s", enabled, reason)
	}

	enabled, reason = flag.Evaluate(UserContext{SubscriptionTier: "silver", Region: "eu-central"})
	if enabled || reason != "default" {
		t.Fatalf("expected default evaluation for no matching rule, got enabled=%v reason=%s", enabled, reason)
	}
}

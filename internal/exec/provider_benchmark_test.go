// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package exec

import (
	"context"
	"testing"

	sdkprovider "github.com/oakwood-commons/scafctl-plugin-sdk/provider"
)

func BenchmarkExecProvider_Execute_DryRun(b *testing.B) {
	p := NewPlugin()

	ctx := sdkprovider.WithDryRun(context.Background(), true)
	inputs := map[string]any{
		"command": "echo hello",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.ExecuteProvider(ctx, ProviderName, inputs)
	}
}

func BenchmarkExecProvider_DescribeWhatIf(b *testing.B) {
	p := NewPlugin()
	ctx := context.Background()
	inputs := map[string]any{
		"command": "echo hello",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.DescribeWhatIf(ctx, ProviderName, inputs)
	}
}

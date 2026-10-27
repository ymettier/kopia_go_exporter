// Copyright 2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package kopiametrics

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Parses valid, missing, malformed, and duplicate release checksum entries.
func TestParseKopiaChecksum(t *testing.T) {
	checksum := strings.Repeat("a", sha256.Size*2)
	tests := []struct {
		name    string
		body    string
		want    string
		wantErr string
	}{
		{
			name: "valid entry",
			body: checksum + "  kopia-0.23.1-linux-x64.tar.gz\n",
			want: checksum,
		},
		{
			name:    "missing entry",
			body:    checksum + "  another-asset.tar.gz\n",
			wantErr: "no checksum entry found",
		},
		{
			name:    "malformed checksum",
			body:    "not-a-sha256  kopia-0.23.1-linux-x64.tar.gz\n",
			wantErr: "invalid SHA256 checksum",
		},
		{
			name:    "duplicate entry",
			body:    checksum + "  kopia-0.23.1-linux-x64.tar.gz\n" + checksum + "  kopia-0.23.1-linux-x64.tar.gz\n",
			wantErr: "multiple checksum entries",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseKopiaChecksum([]byte(tt.body), "kopia-0.23.1-linux-x64.tar.gz")
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Accepts the pinned checksum manifest digest and rejects modified content.
func TestVerifyChecksumManifest(t *testing.T) {
	body := []byte("trusted checksum manifest")
	sum := sha256.Sum256(body)
	expected := hex.EncodeToString(sum[:])

	require.NoError(t, verifyChecksumManifest(body, expected))
	assert.ErrorContains(t, verifyChecksumManifest([]byte("modified manifest"), expected), "SHA256 mismatch")
}

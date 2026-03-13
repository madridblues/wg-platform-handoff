package gateway

import (
	"strings"
	"testing"
)

func TestParseDesiredPeersFiltersAndDedupes(t *testing.T) {
	peers, err := parseDesiredPeers([]map[string]any{
		{
			"public_key":  "peer-a",
			"allowed_ips": []any{"10.64.0.2/32", "fd00::2/128"},
		},
		{
			"public_key":  "peer-a",
			"allowed_ips": []string{"10.64.0.3/32"},
			"preshared_key": "psk-1",
		},
		{
			"public_key":  "",
			"allowed_ips": []any{"10.64.0.4/32"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(peers) != 1 {
		t.Fatalf("expected 1 deduped peer, got %d", len(peers))
	}

	if peers[0].PublicKey != "peer-a" {
		t.Fatalf("expected peer-a, got %s", peers[0].PublicKey)
	}

	if len(peers[0].AllowedIPs) != 1 || peers[0].AllowedIPs[0] != "10.64.0.3/32" {
		t.Fatalf("expected newest deduped allowed IPs, got %+v", peers[0].AllowedIPs)
	}
	if peers[0].Preshared != "psk-1" {
		t.Fatalf("expected preshared key to be preserved")
	}
}

func TestRenderWireGuardConfig(t *testing.T) {
	conf := renderWireGuardConfig("private-key", 51820, []wireGuardPeer{
		{
			PublicKey:  "peer-a",
			Preshared:  "psk-1",
			AllowedIPs: []string{"10.64.0.2/32", "fd00::2/128"},
		},
	})

	expectations := []string{
		"[Interface]",
		"PrivateKey = private-key",
		"ListenPort = 51820",
		"[Peer]",
		"PublicKey = peer-a",
		"PresharedKey = psk-1",
		"AllowedIPs = 10.64.0.2/32, fd00::2/128",
	}

	for _, expected := range expectations {
		if !strings.Contains(conf, expected) {
			t.Fatalf("expected config to contain %q, got:\n%s", expected, conf)
		}
	}
}

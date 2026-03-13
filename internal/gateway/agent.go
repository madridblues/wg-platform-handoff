package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"wg-platform-handoff/internal/config"
	"wg-platform-handoff/internal/domain"
)

type commandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
	RunWithInput(ctx context.Context, input []byte, name string, args ...string) ([]byte, error)
}

type systemCommandRunner struct{}

func (r systemCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

func (r systemCommandRunner) RunWithInput(ctx context.Context, input []byte, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(input)
	return cmd.CombinedOutput()
}

type wireGuardPeer struct {
	PublicKey  string
	AllowedIPs []string
}

type Agent struct {
	cfg                config.Config
	client             *http.Client
	runner             commandRunner
	lastAppliedVersion int64
}

func NewAgent(cfg config.Config) *Agent {
	return &Agent{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
		runner: systemCommandRunner{},
	}
}

func NewAgentWithRunner(cfg config.Config, runner commandRunner) *Agent {
	return &Agent{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
		runner: runner,
	}
}

func (a *Agent) Run(ctx context.Context) error {
	if err := a.register(ctx); err != nil {
		return err
	}

	if err := a.fetchAndApply(ctx); err != nil {
		log.Printf("gateway-agent: initial apply failed: %v", err)
	}

	ticker := time.NewTicker(a.cfg.GatewayHeartbeatPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := a.heartbeat(ctx); err != nil {
				log.Printf("gateway-agent: heartbeat error: %v", err)
			}
			if err := a.fetchAndApply(ctx); err != nil {
				log.Printf("gateway-agent: apply error: %v", err)
			}
		}
	}
}

func (a *Agent) register(ctx context.Context) error {
	hostname, _ := os.Hostname()
	if strings.TrimSpace(hostname) == "" {
		hostname = a.cfg.GatewayID
	}

	wgPublicKey, err := a.resolveWGPublicKey(ctx)
	if err != nil {
		log.Printf("gateway-agent: unable to resolve wg public key during register: %v", err)
	}

	metadata := map[string]string{
		"mode":           "runtime",
		"provider":       a.cfg.GatewayProvider,
		"public_ipv4":    strings.TrimSpace(a.cfg.GatewayPublicIPv4),
		"public_ipv6":    strings.TrimSpace(a.cfg.GatewayPublicIPv6),
		"wg_public_key":  strings.TrimSpace(wgPublicKey),
		"wg_interface":   strings.TrimSpace(a.cfg.GatewayWGInterface),
		"wg_listen_port": strconv.Itoa(a.cfg.GatewayWGListenPort),
	}

	payload := map[string]any{
		"id":       a.cfg.GatewayID,
		"region":   a.cfg.GatewayRegion,
		"hostname": hostname,
		"metadata": metadata,
	}

	return a.postJSON(ctx, "/internal/gateways/register", payload)
}

func (a *Agent) heartbeat(ctx context.Context) error {
	endpoint := fmt.Sprintf("/internal/gateways/%s/heartbeat", a.cfg.GatewayID)
	payload := map[string]any{
		"status": "healthy",
		"metrics": map[string]any{
			"apply_enabled":   a.cfg.GatewayWGApplyEnabled,
			"applied_version": a.lastAppliedVersion,
		},
	}
	return a.postJSON(ctx, endpoint, payload)
}

func (a *Agent) fetchAndApply(ctx context.Context) error {
	desired, err := a.fetchDesiredConfig(ctx)
	if err != nil {
		return err
	}

	if desired.Version > 0 && desired.Version <= a.lastAppliedVersion {
		return nil
	}

	applyErr := a.applyDesiredConfig(ctx, desired)

	resultEndpoint := fmt.Sprintf("/internal/gateways/%s/apply-result", a.cfg.GatewayID)
	payload := map[string]any{
		"desired_version": desired.Version,
	}
	if applyErr != nil {
		payload["result"] = "failed"
		payload["error_text"] = applyErr.Error()
	} else {
		payload["result"] = "success"
	}

	postErr := a.postJSON(ctx, resultEndpoint, payload)
	if applyErr != nil && postErr != nil {
		return fmt.Errorf("apply error: %v; post apply-result error: %v", applyErr, postErr)
	}
	if applyErr != nil {
		return applyErr
	}
	if postErr != nil {
		return postErr
	}

	if desired.Version > 0 {
		a.lastAppliedVersion = desired.Version
	}

	return nil
}

func (a *Agent) fetchDesiredConfig(ctx context.Context) (domain.GatewayDesiredConfigResponse, error) {
	endpoint := fmt.Sprintf("%s/internal/gateways/%s/desired-config", a.cfg.ControlPlaneBaseURL, a.cfg.GatewayID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return domain.GatewayDesiredConfigResponse{}, err
	}
	req.Header.Set("X-Gateway-Token", a.cfg.GatewayToken)

	res, err := a.client.Do(req)
	if err != nil {
		return domain.GatewayDesiredConfigResponse{}, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 8192))
		return domain.GatewayDesiredConfigResponse{}, fmt.Errorf("desired-config status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var desired domain.GatewayDesiredConfigResponse
	if err := json.NewDecoder(res.Body).Decode(&desired); err != nil {
		return domain.GatewayDesiredConfigResponse{}, fmt.Errorf("decode desired-config: %w", err)
	}

	return desired, nil
}

func (a *Agent) applyDesiredConfig(ctx context.Context, desired domain.GatewayDesiredConfigResponse) error {
	if !a.cfg.GatewayWGApplyEnabled {
		return nil
	}

	peers, err := parseDesiredPeers(desired.Peers)
	if err != nil {
		return err
	}

	privateKey, err := loadWireGuardPrivateKey(a.cfg.GatewayWGPrivateKeyPath)
	if err != nil {
		return err
	}

	listenPort := a.cfg.GatewayWGListenPort
	if raw, ok := desired.Relay["listen_port"]; ok {
		parsed, parseErr := strconv.Atoi(strings.TrimSpace(raw))
		if parseErr == nil && parsed > 0 {
			listenPort = parsed
		}
	}

	configText := renderWireGuardConfig(privateKey, listenPort, peers)
	configPath, err := a.writeTempConfig(configText)
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(configPath)
	}()

	if err := a.ensureInterface(ctx); err != nil {
		return err
	}
	if err := a.ensureAddresses(ctx); err != nil {
		return err
	}
	if err := a.runCommand(ctx, "wg", "syncconf", a.cfg.GatewayWGInterface, configPath); err != nil {
		return err
	}
	if err := a.runCommand(ctx, "ip", "link", "set", "up", "dev", a.cfg.GatewayWGInterface); err != nil {
		return err
	}

	return nil
}

func (a *Agent) ensureInterface(ctx context.Context) error {
	iface := a.cfg.GatewayWGInterface
	if strings.TrimSpace(iface) == "" {
		return fmt.Errorf("gateway interface is empty")
	}

	if _, err := a.runner.Run(ctx, "ip", "link", "show", "dev", iface); err == nil {
		return nil
	}

	if err := a.runCommand(ctx, "ip", "link", "add", "dev", iface, "type", "wireguard"); err != nil {
		return fmt.Errorf("create interface %s: %w", iface, err)
	}

	return nil
}

func (a *Agent) ensureAddresses(ctx context.Context) error {
	iface := a.cfg.GatewayWGInterface

	if ipv4 := strings.TrimSpace(a.cfg.GatewayWGAddressIPv4); ipv4 != "" {
		if err := a.runCommand(ctx, "ip", "-4", "address", "replace", ipv4, "dev", iface); err != nil {
			return fmt.Errorf("set ipv4 address: %w", err)
		}
	}

	if ipv6 := strings.TrimSpace(a.cfg.GatewayWGAddressIPv6); ipv6 != "" {
		if err := a.runCommand(ctx, "ip", "-6", "address", "replace", ipv6, "dev", iface); err != nil {
			return fmt.Errorf("set ipv6 address: %w", err)
		}
	}

	return nil
}

func (a *Agent) writeTempConfig(configText string) (string, error) {
	dir := strings.TrimSpace(a.cfg.GatewayWGConfigDir)
	if dir == "" {
		return "", fmt.Errorf("gateway config dir is empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}

	path := filepath.Join(dir, fmt.Sprintf("%s-%d.conf", a.cfg.GatewayWGInterface, time.Now().UnixNano()))
	if err := os.WriteFile(path, []byte(configText), 0o600); err != nil {
		return "", fmt.Errorf("write temp config: %w", err)
	}

	return path, nil
}

func (a *Agent) runCommand(ctx context.Context, name string, args ...string) error {
	output, err := a.runner.Run(ctx, name, args...)
	if err != nil {
		return fmt.Errorf("command failed: %s %s: %w (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (a *Agent) resolveWGPublicKey(ctx context.Context) (string, error) {
	if explicit := strings.TrimSpace(a.cfg.GatewayWGPublicKey); explicit != "" {
		return explicit, nil
	}

	privateKey, err := loadWireGuardPrivateKey(a.cfg.GatewayWGPrivateKeyPath)
	if err != nil {
		return "", err
	}

	output, err := a.runner.RunWithInput(ctx, []byte(privateKey), "wg", "pubkey")
	if err != nil {
		return "", fmt.Errorf("derive wg public key: %w (%s)", err, strings.TrimSpace(string(output)))
	}

	publicKey := strings.TrimSpace(string(output))
	if publicKey == "" {
		return "", fmt.Errorf("derived wg public key is empty")
	}

	return publicKey, nil
}

func parseDesiredPeers(rawPeers []map[string]any) ([]wireGuardPeer, error) {
	dedupe := map[string]wireGuardPeer{}

	for _, raw := range rawPeers {
		publicKey := strings.TrimSpace(stringFromAny(raw["public_key"]))
		if publicKey == "" {
			continue
		}

		allowedIPs := toStringSlice(raw["allowed_ips"])
		if len(allowedIPs) == 0 {
			continue
		}

		dedupe[publicKey] = wireGuardPeer{
			PublicKey:  publicKey,
			AllowedIPs: allowedIPs,
		}
	}

	peers := make([]wireGuardPeer, 0, len(dedupe))
	for _, peer := range dedupe {
		peers = append(peers, peer)
	}

	sort.Slice(peers, func(i, j int) bool {
		return peers[i].PublicKey < peers[j].PublicKey
	})

	return peers, nil
}

func renderWireGuardConfig(privateKey string, listenPort int, peers []wireGuardPeer) string {
	var builder strings.Builder

	builder.WriteString("# generated by gateway-agent\n")
	builder.WriteString("[Interface]\n")
	builder.WriteString("PrivateKey = ")
	builder.WriteString(privateKey)
	builder.WriteString("\n")
	builder.WriteString("ListenPort = ")
	builder.WriteString(strconv.Itoa(listenPort))
	builder.WriteString("\n\n")

	for _, peer := range peers {
		builder.WriteString("[Peer]\n")
		builder.WriteString("PublicKey = ")
		builder.WriteString(peer.PublicKey)
		builder.WriteString("\n")
		builder.WriteString("AllowedIPs = ")
		builder.WriteString(strings.Join(peer.AllowedIPs, ", "))
		builder.WriteString("\n\n")
	}

	return builder.String()
}

func loadWireGuardPrivateKey(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("wireguard private key path is empty")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read wireguard private key: %w", err)
	}

	value := strings.TrimSpace(string(content))
	if value == "" {
		return "", fmt.Errorf("wireguard private key is empty")
	}

	return value, nil
}

func stringFromAny(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

func toStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			trimmed := strings.TrimSpace(item)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out

	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if str, ok := item.(string); ok {
				trimmed := strings.TrimSpace(str)
				if trimmed != "" {
					out = append(out, trimmed)
				}
			}
		}
		return out

	default:
		return nil
	}
}

func (a *Agent) postJSON(ctx context.Context, path string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}

	endpoint := a.cfg.ControlPlaneBaseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gateway-Token", a.cfg.GatewayToken)

	res, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 {
		bodyText, _ := io.ReadAll(io.LimitReader(res.Body, 8192))
		return fmt.Errorf("unexpected status code %d for %s: %s", res.StatusCode, path, strings.TrimSpace(string(bodyText)))
	}
	return nil
}

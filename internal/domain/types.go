package domain

import "time"

type AccessTokenResponse struct {
	AccessToken string    `json:"access_token"`
	Expiry      time.Time `json:"expiry"`
}

type Account struct {
	ID     string    `json:"id"`
	Number string    `json:"number,omitempty"`
	Status string    `json:"status,omitempty"`
	Expiry time.Time `json:"expiry"`
}

type Device struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	PubKey      string    `json:"pubkey"`
	HijackDNS   bool      `json:"hijack_dns"`
	Created     time.Time `json:"created"`
	IPv4Address string    `json:"ipv4_address"`
	IPv6Address string    `json:"ipv6_address"`
}

type VoucherSubmission struct {
	TimeAdded uint64    `json:"time_added"`
	NewExpiry time.Time `json:"new_expiry"`
}

type RelayListResponse struct {
	Locations map[string]RelayLocation `json:"locations"`
	Wireguard WireguardBlock           `json:"wireguard"`
	Bridge    BridgeBlock              `json:"bridge"`
}

type RelayLocation struct {
	City      string  `json:"city"`
	Country   string  `json:"country"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type WireguardBlock struct {
	PortRanges [][]uint16            `json:"port_ranges"`
	IPv4GW     string                `json:"ipv4_gateway"`
	IPv6GW     string                `json:"ipv6_gateway"`
	Relays     []WireguardRelayEntry `json:"relays"`
}

type WireguardRelayEntry struct {
	Hostname   string `json:"hostname"`
	Active     bool   `json:"active"`
	Owned      bool   `json:"owned"`
	Location   string `json:"location"`
	Provider   string `json:"provider"`
	IPv4AddrIn string `json:"ipv4_addr_in"`
	IPv6AddrIn string `json:"ipv6_addr_in,omitempty"`
	Weight     int    `json:"weight"`
	IncludeInC bool   `json:"include_in_country"`
	PublicKey  string `json:"public_key"`
}

type BridgeBlock struct {
	Shadowsocks []map[string]any `json:"shadowsocks"`
	Relays      []BridgeRelay    `json:"relays"`
}

type BridgeRelay struct {
	Hostname   string `json:"hostname"`
	Active     bool   `json:"active"`
	Owned      bool   `json:"owned"`
	Location   string `json:"location"`
	Provider   string `json:"provider"`
	IPv4AddrIn string `json:"ipv4_addr_in"`
	IPv6AddrIn string `json:"ipv6_addr_in,omitempty"`
	Weight     int    `json:"weight"`
	IncludeInC bool   `json:"include_in_country"`
}

type GatewayRegisterRequest struct {
	ID       string            `json:"id"`
	Region   string            `json:"region"`
	Hostname string            `json:"hostname"`
	Metadata map[string]string `json:"metadata"`
}

type GatewayHeartbeatRequest struct {
	Status  string         `json:"status"`
	Metrics map[string]any `json:"metrics"`
}

type GatewayApplyResultRequest struct {
	DesiredVersion int64  `json:"desired_version"`
	Result         string `json:"result"`
	ErrorText      string `json:"error_text,omitempty"`
}

type GatewayDesiredConfigResponse struct {
	Version int64             `json:"version"`
	Peers   []map[string]any  `json:"peers"`
	Relay   map[string]string `json:"relay"`
}

type BillingEvent struct {
	Provider         string
	EventID          string
	EventType        string
	AccountNumber    string
	CustomerID       string
	SubscriptionID   string
	Status           string
	CurrentPeriodEnd *time.Time
	Raw              map[string]any
}

type AuditEvent struct {
	ActorType  string
	ActorID    string
	Action     string
	EntityType string
	EntityID   string
	Payload    map[string]any
}

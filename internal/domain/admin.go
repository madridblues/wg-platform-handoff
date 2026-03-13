package domain

import "time"

type AdminAccountSummary struct {
	ID             string
	AccountNumber  string
	SupabaseUserID string
	Status         string
	Expiry         time.Time
	UpdatedAt      time.Time
	DeviceCount    int64
	Plan           string
	PaymentStatus  string
	CurrentPeriodEnd *time.Time
	LastSeenAt     *time.Time
	RxBytesTotal   int64
	TxBytesTotal   int64
}

type AdminGatewaySummary struct {
	ID            string
	Hostname      string
	Region        string
	Provider      string
	Active        bool
	WGPort        int
	PublicIPv4    string
	PublicIPv6    string
	WGPublicKey   string
	LastStatus    string
	LastHeartbeat *time.Time
	LastApply     string
	LastApplyAt   *time.Time
	ConfiguredPeers int64
	ConnectedPeers  int64
}

type AdminDeviceSummary struct {
	ID            string
	AccountID     string
	AccountNumber string
	Name          string
	PubKey        string
	PresharedKey  string
	HijackDNS     bool
	CreatedAt     time.Time
	IPv4Address   string
	IPv6Address   string
	RelayHostname string
	LastSeenAt    *time.Time
	RxBytes       int64
	TxBytes       int64
	Connected     bool
}

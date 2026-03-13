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
}

type AdminGatewaySummary struct {
	ID            string
	Hostname      string
	Region        string
	Provider      string
	Active        bool
	PublicIPv4    string
	PublicIPv6    string
	WGPublicKey   string
	LastStatus    string
	LastHeartbeat *time.Time
}

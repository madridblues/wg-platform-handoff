package store

import (
	"context"

	"wg-platform-handoff/internal/domain"
)

type Store interface {
	GetOrCreateAccountByUserID(ctx context.Context, userID string) (domain.Account, error)
	GetAccountByNumber(ctx context.Context, accountNumber string) (domain.Account, error)

	ListDevices(ctx context.Context, accountID string) ([]domain.Device, error)
	CreateDevice(ctx context.Context, accountID, pubkey string, hijackDNS bool) (domain.Device, error)
	GetDevice(ctx context.Context, accountID, deviceID string) (domain.Device, error)
	ReplaceDeviceKey(ctx context.Context, accountID, deviceID, pubkey string) (domain.Device, error)
	DeleteDevice(ctx context.Context, accountID, deviceID string) error

	CurrentRelayList(ctx context.Context) (domain.RelayListResponse, string, error)

	RegisterGateway(ctx context.Context, req domain.GatewayRegisterRequest) error
	DesiredGatewayConfig(ctx context.Context, gatewayID string) (domain.GatewayDesiredConfigResponse, error)
	HeartbeatGateway(ctx context.Context, gatewayID string, req domain.GatewayHeartbeatRequest) error
	RecordGatewayApplyResult(ctx context.Context, gatewayID string, req domain.GatewayApplyResultRequest) error

	ApplyBillingEvent(ctx context.Context, event domain.BillingEvent) error
	RecordAuditEvent(ctx context.Context, event domain.AuditEvent) error
}

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"wg-platform-handoff/internal/domain"
)

var errNotFound = errors.New("not found")

type Store struct {
	db *sql.DB
}

func New(databaseURL string) (*Store, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("DATABASE_URL is required")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) GetOrCreateAccountByUserID(ctx context.Context, userID string) (domain.Account, error) {
	if strings.TrimSpace(userID) == "" {
		return domain.Account{}, errors.New("missing user id")
	}

	accountNumber := generateAccountNumber()

	const query = `
with inserted as (
    insert into accounts (account_number, supabase_user_id, status, expiry_at)
    values ($1, $2::uuid, 'active', now() + interval '30 days')
    on conflict (supabase_user_id) do nothing
    returning id::text, account_number, status, expiry_at
)
select id, account_number, status, expiry_at from inserted
union all
select id::text, account_number, status, expiry_at
from accounts
where supabase_user_id = $2::uuid
limit 1;
`

	var account domain.Account
	err := s.db.QueryRowContext(ctx, query, accountNumber, userID).Scan(&account.ID, &account.Number, &account.Status, &account.Expiry)
	if err != nil {
		return domain.Account{}, fmt.Errorf("resolve account: %w", err)
	}

	return account, nil
}

func (s *Store) GetAccountByNumber(ctx context.Context, accountNumber string) (domain.Account, error) {
	accountNumber = strings.TrimSpace(accountNumber)
	if accountNumber == "" {
		return domain.Account{}, errors.New("missing account number")
	}

	const query = `
select id::text, account_number, status, expiry_at
from accounts
where account_number = $1
limit 1;
`

	var account domain.Account
	err := s.db.QueryRowContext(ctx, query, accountNumber).Scan(&account.ID, &account.Number, &account.Status, &account.Expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Account{}, errNotFound
	}
	if err != nil {
		return domain.Account{}, fmt.Errorf("get account by number: %w", err)
	}

	return account, nil
}

func (s *Store) ListDevices(ctx context.Context, accountID string) ([]domain.Device, error) {
	const query = `
select
    id::text,
    name,
    pubkey,
    hijack_dns,
    created_at,
    ipv4_address::text,
    ipv6_address::text
from devices
where account_id = $1::uuid
order by created_at desc;
`

	rows, err := s.db.QueryContext(ctx, query, accountID)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()

	devices := make([]domain.Device, 0)
	for rows.Next() {
		var d domain.Device
		if err := rows.Scan(&d.ID, &d.Name, &d.PubKey, &d.HijackDNS, &d.Created, &d.IPv4Address, &d.IPv6Address); err != nil {
			return nil, fmt.Errorf("scan device: %w", err)
		}
		devices = append(devices, d)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate devices: %w", err)
	}

	return devices, nil
}

func (s *Store) CreateDevice(ctx context.Context, accountID, pubkey string, hijackDNS bool) (domain.Device, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Device{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	name := "device-" + uuid.NewString()[:8]

	const query = `
insert into devices (account_id, name, pubkey, hijack_dns, ipv4_address, ipv6_address)
values ($1::uuid, $2, $3, $4, $5::cidr, $6::cidr)
returning id::text, name, pubkey, hijack_dns, created_at, ipv4_address::text, ipv6_address::text;
`

	const maxAttempts = 24
	for attempt := 0; attempt < maxAttempts; attempt++ {
		slot, err := s.allocateDeviceSlot(ctx, tx)
		if err != nil {
			return domain.Device{}, err
		}

		ipv4, ipv6, err := nextDeviceAddressesForSlot(slot)
		if err != nil {
			return domain.Device{}, err
		}

		var d domain.Device
		err = tx.QueryRowContext(ctx, query, accountID, name, pubkey, hijackDNS, ipv4, ipv6).Scan(
			&d.ID,
			&d.Name,
			&d.PubKey,
			&d.HijackDNS,
			&d.Created,
			&d.IPv4Address,
			&d.IPv6Address,
		)
		if err == nil {
			if commitErr := tx.Commit(); commitErr != nil {
				return domain.Device{}, fmt.Errorf("commit create device: %w", commitErr)
			}
			return d, nil
		}

		if shouldRetryDeviceAddressCollision(err) {
			continue
		}

		return domain.Device{}, fmt.Errorf("create device: %w", err)
	}

	return domain.Device{}, errors.New("failed to allocate unique device address slot")
}

func (s *Store) GetDevice(ctx context.Context, accountID, deviceID string) (domain.Device, error) {
	const query = `
select
    id::text,
    name,
    pubkey,
    hijack_dns,
    created_at,
    ipv4_address::text,
    ipv6_address::text
from devices
where id = $1::uuid and account_id = $2::uuid;
`

	var d domain.Device
	err := s.db.QueryRowContext(ctx, query, deviceID, accountID).Scan(
		&d.ID,
		&d.Name,
		&d.PubKey,
		&d.HijackDNS,
		&d.Created,
		&d.IPv4Address,
		&d.IPv6Address,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Device{}, errNotFound
	}
	if err != nil {
		return domain.Device{}, fmt.Errorf("get device: %w", err)
	}

	return d, nil
}

func (s *Store) ReplaceDeviceKey(ctx context.Context, accountID, deviceID, pubkey string) (domain.Device, error) {
	const query = `
update devices
set pubkey = $1, updated_at = now()
where id = $2::uuid and account_id = $3::uuid
returning id::text, name, pubkey, hijack_dns, created_at, ipv4_address::text, ipv6_address::text;
`

	var d domain.Device
	err := s.db.QueryRowContext(ctx, query, pubkey, deviceID, accountID).Scan(
		&d.ID,
		&d.Name,
		&d.PubKey,
		&d.HijackDNS,
		&d.Created,
		&d.IPv4Address,
		&d.IPv6Address,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Device{}, errNotFound
	}
	if err != nil {
		return domain.Device{}, fmt.Errorf("replace device key: %w", err)
	}

	return d, nil
}

func (s *Store) DeleteDevice(ctx context.Context, accountID, deviceID string) error {
	result, err := s.db.ExecContext(ctx, `delete from devices where id = $1::uuid and account_id = $2::uuid`, deviceID, accountID)
	if err != nil {
		return fmt.Errorf("delete device: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete device rows affected: %w", err)
	}
	if affected == 0 {
		return errNotFound
	}

	return nil
}

func (s *Store) CurrentRelayList(ctx context.Context) (domain.RelayListResponse, string, error) {
	const relayQuery = `
select
    region,
    hostname,
    public_ipv4::text,
    coalesce(public_ipv6::text, ''),
    active,
    weight,
    provider,
    coalesce(wg_public_key, '')
from relays
where active = true and coalesce(wg_public_key, '') <> ''
order by region, hostname;
`

	rows, err := s.db.QueryContext(ctx, relayQuery)
	if err != nil {
		return domain.RelayListResponse{}, "", fmt.Errorf("list relays: %w", err)
	}
	defer rows.Close()

	relayEntries := make([]domain.WireguardRelayEntry, 0)
	locations := map[string]domain.RelayLocation{}

	for rows.Next() {
		var (
			region    string
			hostname  string
			ipv4      string
			ipv6      string
			active    bool
			weight    int
			provider  string
			publicKey string
		)

		if err := rows.Scan(&region, &hostname, &ipv4, &ipv6, &active, &weight, &provider, &publicKey); err != nil {
			return domain.RelayListResponse{}, "", fmt.Errorf("scan relay: %w", err)
		}

		if _, exists := locations[region]; !exists {
			city, country := locationFromRegion(region)
			locations[region] = domain.RelayLocation{City: city, Country: country, Latitude: 0, Longitude: 0}
		}

		relayEntries = append(relayEntries, domain.WireguardRelayEntry{
			Hostname:   hostname,
			Active:     active,
			Owned:      provider == "self",
			Location:   region,
			Provider:   provider,
			IPv4AddrIn: ipv4,
			IPv6AddrIn: ipv6,
			Weight:     weight,
			IncludeInC: true,
			PublicKey:  publicKey,
		})
	}

	if err := rows.Err(); err != nil {
		return domain.RelayListResponse{}, "", fmt.Errorf("iterate relays: %w", err)
	}

	var etagVersion string
	if err := s.db.QueryRowContext(ctx, `select coalesce(to_char(max(updated_at), 'YYYYMMDDHH24MISSUS'), '0') from relays`).Scan(&etagVersion); err != nil {
		return domain.RelayListResponse{}, "", fmt.Errorf("relay etag version: %w", err)
	}

	relayList := domain.RelayListResponse{
		Locations: locations,
		Wireguard: domain.WireguardBlock{
			PortRanges: [][]uint16{{51820, 51830}},
			IPv4GW:     "10.64.0.1",
			IPv6GW:     "fd00::1",
			Relays:     relayEntries,
		},
		Bridge: domain.BridgeBlock{},
	}

	etag := fmt.Sprintf(`W/"relay-%s-%d"`, etagVersion, len(relayEntries))
	return relayList, etag, nil
}

func (s *Store) RegisterGateway(ctx context.Context, req domain.GatewayRegisterRequest) error {
	gatewayID := strings.TrimSpace(req.ID)
	if gatewayID == "" {
		return errors.New("gateway id required")
	}

	region := strings.TrimSpace(req.Region)
	if region == "" {
		region = "unknown"
	}

	provider := strings.TrimSpace(req.Metadata["provider"])
	if provider == "" {
		provider = "self"
	}

	publicIPv4 := strings.TrimSpace(req.Metadata["public_ipv4"])
	if publicIPv4 == "" {
		publicIPv4 = "203.0.113.10"
	}

	publicIPv6 := strings.TrimSpace(req.Metadata["public_ipv6"])
	publicKey := strings.TrimSpace(req.Metadata["wg_public_key"])
	listenPort := 51820
	if rawPort := strings.TrimSpace(req.Metadata["wg_listen_port"]); rawPort != "" {
		if parsed, err := strconv.Atoi(rawPort); err == nil && parsed > 0 && parsed <= 65535 {
			listenPort = parsed
		}
	}

	const query = `
insert into relays (region, hostname, public_ipv4, public_ipv6, wg_port, active, weight, provider, wg_public_key)
values ($1, $2, $3::inet, nullif($4, '')::inet, $5, true, 100, $6, nullif($7, ''))
on conflict (hostname) do update
set
    region = excluded.region,
    public_ipv4 = excluded.public_ipv4,
    public_ipv6 = excluded.public_ipv6,
    wg_port = excluded.wg_port,
    active = true,
    provider = excluded.provider,
    wg_public_key = excluded.wg_public_key,
    updated_at = now();
`

	if _, err := s.db.ExecContext(ctx, query, region, gatewayID, publicIPv4, publicIPv6, listenPort, provider, publicKey); err != nil {
		return fmt.Errorf("register gateway: %w", err)
	}

	return nil
}

func (s *Store) DesiredGatewayConfig(ctx context.Context, gatewayID string) (domain.GatewayDesiredConfigResponse, error) {
	const peerQuery = `
select pubkey, ipv4_address::text, ipv6_address::text, coalesce(preshared_key, '')
from devices
order by created_at desc
limit 2000;
`

	rows, err := s.db.QueryContext(ctx, peerQuery)
	if err != nil {
		return domain.GatewayDesiredConfigResponse{}, fmt.Errorf("query desired peers: %w", err)
	}
	defer rows.Close()

	peers := make([]map[string]any, 0)
	for rows.Next() {
		var (
			pubkey string
			ipv4   string
			ipv6   string
			psk    string
		)
		if err := rows.Scan(&pubkey, &ipv4, &ipv6, &psk); err != nil {
			return domain.GatewayDesiredConfigResponse{}, fmt.Errorf("scan desired peer: %w", err)
		}
		peer := map[string]any{
			"public_key":  pubkey,
			"allowed_ips": []string{ipv4, ipv6},
		}
		if strings.TrimSpace(psk) != "" {
			peer["preshared_key"] = strings.TrimSpace(psk)
		}
		peers = append(peers, peer)
	}

	if err := rows.Err(); err != nil {
		return domain.GatewayDesiredConfigResponse{}, fmt.Errorf("iterate desired peers: %w", err)
	}

	relayConfig := map[string]string{"gateway_id": gatewayID}
	var listenPort int
	if err := s.db.QueryRowContext(ctx, `select wg_port from relays where hostname = $1`, gatewayID).Scan(&listenPort); err == nil && listenPort > 0 {
		relayConfig["listen_port"] = strconv.Itoa(listenPort)
	}

	return domain.GatewayDesiredConfigResponse{
		Version: time.Now().UTC().Unix(),
		Peers:   peers,
		Relay:   relayConfig,
	}, nil
}

func (s *Store) HeartbeatGateway(ctx context.Context, gatewayID string, req domain.GatewayHeartbeatRequest) error {
	relayID, err := s.relayIDByHostname(ctx, gatewayID)
	if err != nil {
		return err
	}

	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "unknown"
	}

	metrics, err := json.Marshal(req.Metrics)
	if err != nil {
		return fmt.Errorf("marshal heartbeat metrics: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
insert into relay_heartbeats (relay_id, status, metrics)
values ($1::uuid, $2, $3::jsonb)
`, relayID, status, metrics); err != nil {
		return fmt.Errorf("insert relay heartbeat: %w", err)
	}

	if err := s.updateDeviceRuntimeStatsFromMetrics(ctx, relayID, req.Metrics); err != nil {
		return fmt.Errorf("update device runtime stats: %w", err)
	}

	return nil
}

func (s *Store) RecordGatewayApplyResult(ctx context.Context, gatewayID string, req domain.GatewayApplyResultRequest) error {
	relayID, err := s.relayIDByHostname(ctx, gatewayID)
	if err != nil {
		return err
	}

	const query = `
insert into gateway_apply_events (relay_id, desired_version, result, error_text)
values ($1::uuid, $2, $3, $4)
on conflict (relay_id, desired_version) do update
set
    result = excluded.result,
    error_text = excluded.error_text,
    created_at = now();
`

	if _, err := s.db.ExecContext(ctx, query, relayID, req.DesiredVersion, req.Result, req.ErrorText); err != nil {
		return fmt.Errorf("record gateway apply result: %w", err)
	}

	return nil
}

func (s *Store) ApplyBillingEvent(ctx context.Context, event domain.BillingEvent) error {
	provider := normalizeProvider(event.Provider)
	if provider == "" {
		return errors.New("billing provider required")
	}

	rawPayload := event.Raw
	if rawPayload == nil {
		rawPayload = map[string]any{}
	}

	rawJSON, err := json.Marshal(rawPayload)
	if err != nil {
		return fmt.Errorf("marshal billing payload: %w", err)
	}

	eventID := strings.TrimSpace(event.EventID)
	if eventID == "" {
		sum := sha256.Sum256(rawJSON)
		eventID = fmt.Sprintf("%s-%s", provider, hex.EncodeToString(sum[:12]))
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin billing tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	inserted, err := s.markBillingEventReceived(ctx, tx, provider, eventID, event.EventType, rawJSON)
	if err != nil {
		return err
	}
	if !inserted {
		if commitErr := tx.Commit(); commitErr != nil {
			return fmt.Errorf("commit duplicate billing tx: %w", commitErr)
		}
		return nil
	}

	accountID, err := s.resolveAccountIDForBilling(ctx, tx, provider, event)
	if errors.Is(err, errNotFound) {
		// Unknown account mapping: ignore for now and let future events reconcile.
		if commitErr := tx.Commit(); commitErr != nil {
			return fmt.Errorf("commit ignored billing tx: %w", commitErr)
		}
		return nil
	}
	if err != nil {
		return err
	}

	subscriptionID := strings.TrimSpace(event.SubscriptionID)
	if subscriptionID == "" {
		subscriptionID = "unknown-" + provider
	}

	customerID := strings.TrimSpace(event.CustomerID)
	if customerID == "" {
		customerID = "unknown-" + provider
	}

	normalizedStatus := normalizeBillingStatus(event.Status, event.EventType)

	var periodEnd any
	if event.CurrentPeriodEnd != nil {
		periodEnd = event.CurrentPeriodEnd.UTC()
	}

	const upsertSubscription = `
insert into subscriptions (
    account_id,
    provider,
    external_customer_id,
    external_subscription_id,
    status,
    current_period_end,
    last_webhook_event_id,
    updated_at
)
values (
    $1::uuid,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7,
    now()
)
on conflict (provider, external_subscription_id) do update
set
    account_id = excluded.account_id,
    external_customer_id = excluded.external_customer_id,
    status = excluded.status,
    current_period_end = excluded.current_period_end,
    last_webhook_event_id = excluded.last_webhook_event_id,
    updated_at = now();
`

	if _, err := tx.ExecContext(ctx, upsertSubscription, accountID, provider, customerID, subscriptionID, normalizedStatus, periodEnd, eventID); err != nil {
		return fmt.Errorf("upsert subscription: %w", err)
	}

	accountStatus := accountStatusFromBillingStatus(normalizedStatus)
	var expiry any
	if event.CurrentPeriodEnd != nil {
		expiry = event.CurrentPeriodEnd.UTC()
	} else if accountStatus == "suspended" {
		expiry = time.Now().UTC()
	}

	if _, err := tx.ExecContext(ctx, `
update accounts
set
    status = $2,
    expiry_at = coalesce($3::timestamptz, expiry_at),
    updated_at = now()
where id = $1::uuid
`, accountID, accountStatus, expiry); err != nil {
		return fmt.Errorf("update account entitlement: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit billing tx: %w", err)
	}

	return nil
}

func (s *Store) markBillingEventReceived(ctx context.Context, tx *sql.Tx, provider, eventID, eventType string, payload []byte) (bool, error) {
	const query = `
insert into billing_webhook_events (provider, event_id, event_type, payload)
values ($1, $2, $3, $4::jsonb)
on conflict (provider, event_id) do nothing;
`

	result, err := tx.ExecContext(ctx, query, provider, eventID, eventType, payload)
	if err != nil {
		return false, fmt.Errorf("insert billing webhook event: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("billing webhook rows affected: %w", err)
	}

	return affected > 0, nil
}

func (s *Store) RecordAuditEvent(ctx context.Context, event domain.AuditEvent) error {
	actorType := strings.TrimSpace(event.ActorType)
	if actorType == "" {
		actorType = "system"
	}

	actorID := strings.TrimSpace(event.ActorID)
	if actorID == "" {
		actorID = "unknown"
	}

	action := strings.TrimSpace(event.Action)
	if action == "" {
		action = "unknown"
	}

	entityType := strings.TrimSpace(event.EntityType)
	if entityType == "" {
		entityType = "unknown"
	}

	entityID := strings.TrimSpace(event.EntityID)

	payload := event.Payload
	if payload == nil {
		payload = map[string]any{}
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal audit payload: %w", err)
	}

	const query = `
insert into audit_events (actor_type, actor_id, action, entity_type, entity_id, payload)
values ($1, $2, $3, $4, nullif($5, ''), $6::jsonb);
`

	if _, err := s.db.ExecContext(ctx, query, actorType, actorID, action, entityType, entityID, payloadJSON); err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}

	return nil
}

func (s *Store) AdminListAccounts(ctx context.Context, limit int) ([]domain.AdminAccountSummary, error) {
	if limit <= 0 {
		limit = 200
	}

	const query = `
select
    a.id::text,
    a.account_number,
    a.supabase_user_id::text,
    a.status,
    a.expiry_at,
    a.updated_at,
    count(d.id)::bigint as device_count,
    coalesce(sub.provider, 'paddle') as plan,
    coalesce(sub.status, 'unpaid') as payment_status,
    sub.current_period_end,
    usage.last_seen_at,
    coalesce(usage.rx_bytes_total, 0)::bigint,
    coalesce(usage.tx_bytes_total, 0)::bigint
from accounts a
left join devices d on d.account_id = a.id
left join lateral (
    select s.provider, s.status, s.current_period_end
    from subscriptions s
    where s.account_id = a.id
    order by s.updated_at desc
    limit 1
) sub on true
left join lateral (
    select
        max(drs.last_handshake_at) as last_seen_at,
        sum(drs.rx_bytes) as rx_bytes_total,
        sum(drs.tx_bytes) as tx_bytes_total
    from devices d2
    left join device_runtime_stats drs on drs.device_id = d2.id
    where d2.account_id = a.id
) usage on true
group by a.id, a.account_number, a.supabase_user_id, a.status, a.expiry_at, a.updated_at, sub.provider, sub.status, sub.current_period_end, usage.last_seen_at, usage.rx_bytes_total, usage.tx_bytes_total
order by a.updated_at desc
limit $1;
`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("admin list accounts: %w", err)
	}
	defer rows.Close()

	out := make([]domain.AdminAccountSummary, 0)
	for rows.Next() {
		var (
			item             domain.AdminAccountSummary
			currentPeriodEnd sql.NullTime
			lastSeenAt       sql.NullTime
		)
		if err := rows.Scan(
			&item.ID,
			&item.AccountNumber,
			&item.SupabaseUserID,
			&item.Status,
			&item.Expiry,
			&item.UpdatedAt,
			&item.DeviceCount,
			&item.Plan,
			&item.PaymentStatus,
			&currentPeriodEnd,
			&lastSeenAt,
			&item.RxBytesTotal,
			&item.TxBytesTotal,
		); err != nil {
			return nil, fmt.Errorf("admin scan account: %w", err)
		}
		if currentPeriodEnd.Valid {
			t := currentPeriodEnd.Time.UTC()
			item.CurrentPeriodEnd = &t
		}
		if lastSeenAt.Valid {
			t := lastSeenAt.Time.UTC()
			item.LastSeenAt = &t
		}
		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("admin iterate accounts: %w", err)
	}

	return out, nil
}

func (s *Store) AdminListGateways(ctx context.Context, limit int) ([]domain.AdminGatewaySummary, error) {
	if limit <= 0 {
		limit = 200
	}

	const query = `
select
    r.id::text,
    r.hostname,
    r.region,
    r.provider,
    r.active,
    r.wg_port,
    r.public_ipv4::text,
    coalesce(r.public_ipv6::text, ''),
    coalesce(r.wg_public_key, ''),
    coalesce(h.status, 'unknown'),
    h.received_at,
    coalesce(a.result, 'n/a'),
    a.created_at,
    coalesce((h.metrics->>'configured_peers')::bigint, 0)::bigint,
    coalesce((
        select count(*)
        from device_runtime_stats drs
        where drs.relay_id = r.id
          and drs.last_handshake_at > now() - interval '3 minutes'
    ), 0)::bigint as connected_peers
from relays r
left join lateral (
    select status, received_at, metrics
    from relay_heartbeats rh
    where rh.relay_id = r.id
    order by rh.received_at desc
    limit 1
) h on true
left join lateral (
    select result, created_at
    from gateway_apply_events ga
    where ga.relay_id = r.id
    order by ga.created_at desc
    limit 1
) a on true
order by r.updated_at desc
limit $1;
`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("admin list gateways: %w", err)
	}
	defer rows.Close()

	out := make([]domain.AdminGatewaySummary, 0)
	for rows.Next() {
		var (
			item          domain.AdminGatewaySummary
			lastHeartbeat sql.NullTime
			lastApplyAt   sql.NullTime
		)

		if err := rows.Scan(
			&item.ID,
			&item.Hostname,
			&item.Region,
			&item.Provider,
			&item.Active,
			&item.WGPort,
			&item.PublicIPv4,
			&item.PublicIPv6,
			&item.WGPublicKey,
			&item.LastStatus,
			&lastHeartbeat,
			&item.LastApply,
			&lastApplyAt,
			&item.ConfiguredPeers,
			&item.ConnectedPeers,
		); err != nil {
			return nil, fmt.Errorf("admin scan gateway: %w", err)
		}

		if lastHeartbeat.Valid {
			t := lastHeartbeat.Time.UTC()
			item.LastHeartbeat = &t
		}
		if lastApplyAt.Valid {
			t := lastApplyAt.Time.UTC()
			item.LastApplyAt = &t
		}

		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("admin iterate gateways: %w", err)
	}

	return out, nil
}

func (s *Store) AdminListDevices(ctx context.Context, limit int) ([]domain.AdminDeviceSummary, error) {
	if limit <= 0 {
		limit = 1000
	}

	const query = `
select
    d.id::text,
    d.account_id::text,
    a.account_number,
    d.name,
    d.pubkey,
    coalesce(d.preshared_key, ''),
    d.hijack_dns,
    d.created_at,
    d.ipv4_address::text,
    d.ipv6_address::text,
    coalesce(r.hostname, ''),
    drs.last_handshake_at,
    coalesce(drs.rx_bytes, 0)::bigint,
    coalesce(drs.tx_bytes, 0)::bigint,
    coalesce(drs.last_handshake_at > now() - interval '3 minutes', false)
from devices d
join accounts a on a.id = d.account_id
left join device_runtime_stats drs on drs.device_id = d.id
left join relays r on r.id = drs.relay_id
order by d.created_at desc
limit $1;
`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("admin list devices: %w", err)
	}
	defer rows.Close()

	out := make([]domain.AdminDeviceSummary, 0)
	for rows.Next() {
		var (
			item       domain.AdminDeviceSummary
			lastSeenAt sql.NullTime
		)
		if err := rows.Scan(
			&item.ID,
			&item.AccountID,
			&item.AccountNumber,
			&item.Name,
			&item.PubKey,
			&item.PresharedKey,
			&item.HijackDNS,
			&item.CreatedAt,
			&item.IPv4Address,
			&item.IPv6Address,
			&item.RelayHostname,
			&lastSeenAt,
			&item.RxBytes,
			&item.TxBytes,
			&item.Connected,
		); err != nil {
			return nil, fmt.Errorf("admin scan device: %w", err)
		}
		if lastSeenAt.Valid {
			t := lastSeenAt.Time.UTC()
			item.LastSeenAt = &t
		}
		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("admin iterate devices: %w", err)
	}

	return out, nil
}

func (s *Store) AdminGetDeviceByAccountNumber(ctx context.Context, accountNumber, deviceID string) (domain.AdminDeviceSummary, error) {
	accountNumber = strings.TrimSpace(accountNumber)
	deviceID = strings.TrimSpace(deviceID)
	if accountNumber == "" || deviceID == "" {
		return domain.AdminDeviceSummary{}, errors.New("account number and device id are required")
	}

	const query = `
select
    d.id::text,
    d.account_id::text,
    a.account_number,
    d.name,
    d.pubkey,
    coalesce(d.preshared_key, ''),
    d.hijack_dns,
    d.created_at,
    d.ipv4_address::text,
    d.ipv6_address::text,
    coalesce(r.hostname, ''),
    drs.last_handshake_at,
    coalesce(drs.rx_bytes, 0)::bigint,
    coalesce(drs.tx_bytes, 0)::bigint,
    coalesce(drs.last_handshake_at > now() - interval '3 minutes', false)
from devices d
join accounts a on a.id = d.account_id
left join device_runtime_stats drs on drs.device_id = d.id
left join relays r on r.id = drs.relay_id
where a.account_number = $1 and d.id = $2::uuid
limit 1;
`

	var item domain.AdminDeviceSummary
	var lastSeenAt sql.NullTime
	err := s.db.QueryRowContext(ctx, query, accountNumber, deviceID).Scan(
		&item.ID,
		&item.AccountID,
		&item.AccountNumber,
		&item.Name,
		&item.PubKey,
		&item.PresharedKey,
		&item.HijackDNS,
		&item.CreatedAt,
		&item.IPv4Address,
		&item.IPv6Address,
		&item.RelayHostname,
		&lastSeenAt,
		&item.RxBytes,
		&item.TxBytes,
		&item.Connected,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AdminDeviceSummary{}, errNotFound
	}
	if err != nil {
		return domain.AdminDeviceSummary{}, fmt.Errorf("admin get device by account number: %w", err)
	}
	if lastSeenAt.Valid {
		t := lastSeenAt.Time.UTC()
		item.LastSeenAt = &t
	}

	return item, nil
}

func (s *Store) AdminReplaceDeviceKeyByAccountNumber(ctx context.Context, accountNumber, deviceID, pubkey, presharedKey string) (domain.AdminDeviceSummary, error) {
	accountNumber = strings.TrimSpace(accountNumber)
	deviceID = strings.TrimSpace(deviceID)
	pubkey = strings.TrimSpace(pubkey)
	if accountNumber == "" || deviceID == "" || pubkey == "" {
		return domain.AdminDeviceSummary{}, errors.New("account number, device id, and pubkey are required")
	}
	presharedKey = strings.TrimSpace(presharedKey)

	const query = `
with updated as (
update devices d
set
    pubkey = $3,
    preshared_key = nullif($4, ''),
    updated_at = now()
from accounts a
where d.account_id = a.id
  and a.account_number = $1
  and d.id = $2::uuid
returning
    d.id::text,
    d.account_id::text,
    a.account_number,
    d.name,
    d.pubkey,
    coalesce(d.preshared_key, ''),
    d.hijack_dns,
    d.created_at,
    d.ipv4_address::text,
    d.ipv6_address::text
)
select
    u.id,
    u.account_id,
    u.account_number,
    u.name,
    u.pubkey,
    u.preshared_key,
    u.hijack_dns,
    u.created_at,
    u.ipv4_address,
    u.ipv6_address,
    coalesce(r.hostname, ''),
    drs.last_handshake_at,
    coalesce(drs.rx_bytes, 0)::bigint,
    coalesce(drs.tx_bytes, 0)::bigint,
    coalesce(drs.last_handshake_at > now() - interval '3 minutes', false)
from updated u
left join device_runtime_stats drs on drs.device_id::text = u.id
left join relays r on r.id = drs.relay_id;
`

	var item domain.AdminDeviceSummary
	var lastSeenAt sql.NullTime
	err := s.db.QueryRowContext(ctx, query, accountNumber, deviceID, pubkey, presharedKey).Scan(
		&item.ID,
		&item.AccountID,
		&item.AccountNumber,
		&item.Name,
		&item.PubKey,
		&item.PresharedKey,
		&item.HijackDNS,
		&item.CreatedAt,
		&item.IPv4Address,
		&item.IPv6Address,
		&item.RelayHostname,
		&lastSeenAt,
		&item.RxBytes,
		&item.TxBytes,
		&item.Connected,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AdminDeviceSummary{}, errNotFound
	}
	if err != nil {
		return domain.AdminDeviceSummary{}, fmt.Errorf("admin replace device key by account number: %w", err)
	}
	if lastSeenAt.Valid {
		t := lastSeenAt.Time.UTC()
		item.LastSeenAt = &t
	}

	return item, nil
}

func (s *Store) updateDeviceRuntimeStatsFromMetrics(ctx context.Context, relayID string, metrics map[string]any) error {
	if len(metrics) == 0 {
		return nil
	}

	rawPeers, ok := metrics["peers"]
	if !ok {
		return nil
	}

	peerItems, ok := rawPeers.([]any)
	if !ok {
		return nil
	}

	const upsertQuery = `
insert into device_runtime_stats (device_id, relay_id, endpoint, last_handshake_at, rx_bytes, tx_bytes, updated_at)
values ($1::uuid, $2::uuid, nullif($3, ''), $4, $5, $6, now())
on conflict (device_id) do update
set
    relay_id = excluded.relay_id,
    endpoint = excluded.endpoint,
    last_handshake_at = excluded.last_handshake_at,
    rx_bytes = excluded.rx_bytes,
    tx_bytes = excluded.tx_bytes,
    updated_at = now();
`

	for _, item := range peerItems {
		peerMap, ok := item.(map[string]any)
		if !ok {
			continue
		}

		publicKey := strings.TrimSpace(toMetricString(peerMap["public_key"]))
		if publicKey == "" {
			continue
		}

		var deviceID string
		if err := s.db.QueryRowContext(ctx, `select id::text from devices where pubkey = $1`, publicKey).Scan(&deviceID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return fmt.Errorf("lookup device by public key: %w", err)
		}

		endpoint := strings.TrimSpace(toMetricString(peerMap["endpoint"]))
		lastHandshake := toMetricTime(peerMap["latest_handshake"])
		rxBytes := toMetricInt64(peerMap["rx_bytes"])
		txBytes := toMetricInt64(peerMap["tx_bytes"])

		if _, err := s.db.ExecContext(ctx, upsertQuery, deviceID, relayID, endpoint, lastHandshake, rxBytes, txBytes); err != nil {
			return fmt.Errorf("upsert device runtime stats: %w", err)
		}
	}

	return nil
}

func (s *Store) resolveAccountIDForBilling(ctx context.Context, tx *sql.Tx, provider string, event domain.BillingEvent) (string, error) {
	if accountNumber := strings.TrimSpace(event.AccountNumber); accountNumber != "" {
		var accountID string
		err := tx.QueryRowContext(ctx, `select id::text from accounts where account_number = $1`, accountNumber).Scan(&accountID)
		if err == nil {
			return accountID, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("lookup account by number: %w", err)
		}
	}

	if subscriptionID := strings.TrimSpace(event.SubscriptionID); subscriptionID != "" {
		var accountID string
		err := tx.QueryRowContext(ctx, `
select account_id::text
from subscriptions
where provider = $1 and external_subscription_id = $2
limit 1
`, provider, subscriptionID).Scan(&accountID)
		if err == nil {
			return accountID, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("lookup account by subscription: %w", err)
		}
	}

	if customerID := strings.TrimSpace(event.CustomerID); customerID != "" {
		var accountID string
		err := tx.QueryRowContext(ctx, `
select account_id::text
from subscriptions
where provider = $1 and external_customer_id = $2
order by updated_at desc
limit 1
`, provider, customerID).Scan(&accountID)
		if err == nil {
			return accountID, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("lookup account by customer: %w", err)
		}
	}

	return "", errNotFound
}

func (s *Store) relayIDByHostname(ctx context.Context, hostname string) (string, error) {
	var relayID string
	err := s.db.QueryRowContext(ctx, `select id::text from relays where hostname = $1`, hostname).Scan(&relayID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("relay %q not found", hostname)
	}
	if err != nil {
		return "", fmt.Errorf("lookup relay by hostname: %w", err)
	}
	return relayID, nil
}

func (s *Store) allocateDeviceSlot(ctx context.Context, tx *sql.Tx) (int64, error) {
	var slot int64
	err := tx.QueryRowContext(ctx, `select nextval('device_ip_slot_seq')`).Scan(&slot)
	if err == nil {
		return slot, nil
	}

	if !isSQLState(err, "42P01") && !isSQLState(err, "42704") {
		return 0, fmt.Errorf("allocate device slot: %w", err)
	}

	// Compatibility fallback for environments where migration 004 has not been applied yet.
	var count int64
	if err := tx.QueryRowContext(ctx, `select count(*) from devices`).Scan(&count); err != nil {
		return 0, fmt.Errorf("fallback device slot allocation: %w", err)
	}

	return count + 1, nil
}

func nextDeviceAddressesForSlot(slot int64) (string, string, error) {
	if slot <= 0 {
		return "", "", errors.New("invalid device slot")
	}

	third := (slot - 1) / 253
	fourth := ((slot - 1) % 253) + 2

	if third > 255 {
		return "", "", errors.New("device address pool exhausted")
	}

	ipv4 := fmt.Sprintf("10.64.%d.%d/32", third, fourth)
	ipv6 := fmt.Sprintf("fd00:%x::%x/128", third, fourth)
	return ipv4, ipv6, nil
}

func shouldRetryDeviceAddressCollision(err error) bool {
	pgErr, ok := asPgError(err)
	if !ok || pgErr.Code != "23505" {
		return false
	}

	constraint := strings.ToLower(strings.TrimSpace(pgErr.ConstraintName))
	if strings.Contains(constraint, "ipv4") || strings.Contains(constraint, "ipv6") {
		return true
	}
	return false
}

func asPgError(err error) (*pgconn.PgError, bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr, true
	}
	return nil, false
}

func isSQLState(err error, sqlState string) bool {
	pgErr, ok := asPgError(err)
	if !ok {
		return false
	}
	return pgErr.Code == sqlState
}

func toMetricString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return ""
	}
}

func toMetricInt64(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case int32:
		return int64(typed)
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case json.Number:
		if v, err := typed.Int64(); err == nil {
			return v
		}
		if v, err := typed.Float64(); err == nil {
			return int64(v)
		}
	case string:
		v := strings.TrimSpace(typed)
		if v == "" {
			return 0
		}
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			return parsed
		}
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			return int64(parsed)
		}
	}
	return 0
}

func toMetricTime(value any) any {
	switch typed := value.(type) {
	case time.Time:
		t := typed.UTC()
		return t
	case float64:
		if typed <= 0 {
			return nil
		}
		t := time.Unix(int64(typed), 0).UTC()
		return t
	case int64:
		if typed <= 0 {
			return nil
		}
		t := time.Unix(typed, 0).UTC()
		return t
	case int:
		if typed <= 0 {
			return nil
		}
		t := time.Unix(int64(typed), 0).UTC()
		return t
	case json.Number:
		if v, err := typed.Int64(); err == nil && v > 0 {
			t := time.Unix(v, 0).UTC()
			return t
		}
		if v, err := typed.Float64(); err == nil && v > 0 {
			t := time.Unix(int64(v), 0).UTC()
			return t
		}
	case string:
		v := strings.TrimSpace(typed)
		if v == "" {
			return nil
		}
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil && parsed > 0 {
			t := time.Unix(parsed, 0).UTC()
			return t
		}
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			t := parsed.UTC()
			return t
		}
	}
	return nil
}

func generateAccountNumber() string {
	// 16 uppercase chars gives us a compact account token for MVP UX.
	return strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))[:16]
}

func locationFromRegion(region string) (city string, country string) {
	parts := strings.Split(region, "-")
	if len(parts) == 1 {
		return strings.ToUpper(region), "Unknown"
	}

	country = strings.ToUpper(parts[0])
	cityPart := parts[len(parts)-1]
	if cityPart == "" {
		cityPart = "unknown"
	}

	city = strings.ToUpper(cityPart[:1]) + cityPart[1:]
	return city, country
}

func normalizeProvider(provider string) string {
	value := strings.ToLower(strings.TrimSpace(provider))
	switch value {
	case "paddle":
		return value
	default:
		return ""
	}
}

func normalizeBillingStatus(status, eventType string) string {
	value := strings.ToLower(strings.TrimSpace(status))
	if value != "" && value != "unknown" {
		return value
	}

	event := strings.ToLower(strings.TrimSpace(eventType))
	switch {
	case strings.Contains(event, "payment_failed"):
		return "past_due"
	case strings.Contains(event, "deleted"), strings.Contains(event, "canceled"), strings.Contains(event, "cancelled"):
		return "canceled"
	default:
		return "active"
	}
}

func accountStatusFromBillingStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active", "trialing", "past_due":
		return "active"
	default:
		return "suspended"
	}
}

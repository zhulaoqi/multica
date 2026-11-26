package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const cliDeviceSessionPrefix = "mul:auth:cli-device:"

var cliDeviceCreateScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 1 then
    return 0
end
redis.call('HSET', KEYS[1], 'd', ARGV[1])
redis.call('EXPIRE', KEYS[1], ARGV[2])
return 1
`)

var cliDeviceApproveScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
    return 0
end
local approved_user = redis.call('HGET', KEYS[1], 'u')
if approved_user then
    if approved_user == ARGV[1] then
        return 1
    end
    return 2
end
redis.call('HSET', KEYS[1], 'u', ARGV[1])
return 1
`)

var cliDeviceCompleteScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
    return ''
end
if redis.call('HGET', KEYS[1], 'd') ~= ARGV[1] then
    return ''
end
local approved_user = redis.call('HGET', KEYS[1], 'u')
if not approved_user then
    return ''
end
local token = redis.call('HGET', KEYS[1], 't')
if not token then
    token = ARGV[2]
    redis.call('HSET', KEYS[1], 't', token)
end
redis.call('EXPIRE', KEYS[1], ARGV[3])
return token
`)

type RedisCLIDeviceStore struct {
	rdb redis.UniversalClient
}

func NewRedisCLIDeviceStore(rdb redis.UniversalClient) *RedisCLIDeviceStore {
	return &RedisCLIDeviceStore{rdb: rdb}
}

func cliDeviceSessionKey(userCode string) string {
	return cliDeviceSessionPrefix + "{" + userCode + "}"
}

func cliDeviceTTLSeconds(ttl time.Duration) int {
	seconds := int((ttl + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

func (s *RedisCLIDeviceStore) Create(ctx context.Context, deviceDigest, userCode string, ttl time.Duration) error {
	created, err := cliDeviceCreateScript.Run(
		ctx,
		s.rdb,
		[]string{cliDeviceSessionKey(userCode)},
		deviceDigest,
		cliDeviceTTLSeconds(ttl),
	).Int64()
	if err != nil {
		return fmt.Errorf("auth: create cli device login: %w", err)
	}
	if created == 0 {
		return ErrCLIDeviceCodeConflict
	}
	return nil
}

func (s *RedisCLIDeviceStore) Approve(ctx context.Context, userCode, userID string) error {
	result, err := cliDeviceApproveScript.Run(
		ctx,
		s.rdb,
		[]string{cliDeviceSessionKey(userCode)},
		userID,
	).Int64()
	if err != nil {
		return fmt.Errorf("auth: approve cli device login: %w", err)
	}
	switch result {
	case 0:
		return ErrCLIDeviceNotFound
	case 2:
		return ErrCLIDeviceAlreadyApproved
	default:
		return nil
	}
}

func (s *RedisCLIDeviceStore) Get(ctx context.Context, userCode, deviceDigest string) (CLIDeviceApproval, error) {
	fields, err := s.rdb.HGetAll(ctx, cliDeviceSessionKey(userCode)).Result()
	if err != nil {
		return CLIDeviceApproval{}, fmt.Errorf("auth: load cli device login: %w", err)
	}
	if len(fields) == 0 || fields["d"] != deviceDigest {
		return CLIDeviceApproval{}, ErrCLIDeviceNotFound
	}
	if fields["u"] == "" {
		return CLIDeviceApproval{}, ErrCLIDevicePending
	}
	return CLIDeviceApproval{UserID: fields["u"], Token: fields["t"]}, nil
}

func (s *RedisCLIDeviceStore) Complete(ctx context.Context, userCode, deviceDigest, token string, ttl time.Duration) (string, error) {
	storedToken, err := cliDeviceCompleteScript.Run(
		ctx,
		s.rdb,
		[]string{cliDeviceSessionKey(userCode)},
		deviceDigest,
		token,
		cliDeviceTTLSeconds(ttl),
	).Text()
	if err != nil {
		return "", fmt.Errorf("auth: complete cli device login: %w", err)
	}
	if storedToken == "" {
		return "", ErrCLIDeviceNotFound
	}
	return storedToken, nil
}

var _ CLIDeviceStore = (*RedisCLIDeviceStore)(nil)

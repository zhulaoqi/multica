package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/integrations/lark"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The organization bot reuses the Feishu login app as a single company-wide
// entry point: one long connection, one bot, every project. It is independent
// of MULTICA_LARK_SECRET_KEY, which gates per-agent bots whose secrets are
// encrypted at rest; this bot's credentials come from deployment env and are
// never stored.
const (
	orgBotLeaseTTL       = 180 * time.Second
	orgBotLeaseRenew     = 60 * time.Second
	orgBotLeasePoll      = 30 * time.Second
	orgBotMinBackoff     = 5 * time.Second
	orgBotMaxBackoff     = 5 * time.Minute
	orgBotResetBackoffIn = time.Minute
	orgBotEnsureTimeout  = 15 * time.Second

	// OrgBot env names are shared with Feishu login, so enabling login already
	// provides the app identity. The Feishu console additionally needs the
	// app's bot capability and long-connection event subscription.
	orgBotAppIDEnv     = "MULTICA_FEISHU_APP_ID"
	orgBotAppSecretEnv = "MULTICA_FEISHU_APP_SECRET"
)

// feishuOrgBotWorker registers the installation lazily (so a Feishu outage at
// boot retries instead of disabling the bot) and owns lease fencing plus
// reconnect backoff around the bot's single long connection.
type feishuOrgBotWorker struct {
	queries     *db.Queries
	dispatcher  engine.OrgDispatcher
	router      lark.OrgBotRouter
	chat        lark.OrgBotChatStore
	attachments lark.OrgBotAttachmentStore
	client      lark.APIClient
	connector   lark.EventConnector
	creds       lark.InstallationCredentials
	appURL      string
	logger      *slog.Logger

	bot            *lark.OrgBot
	installationID pgtype.UUID
}

var _ lark.OrgBotProjects = (*service.OrgDispatchService)(nil)

// registerFeishuOrgBot wires the organization bot when the Feishu login app is
// configured. Returns nil (and logs why) otherwise.
func registerFeishuOrgBot(h *handler.Handler, queries *db.Queries, appURL string) *feishuOrgBotWorker {
	appID := strings.TrimSpace(os.Getenv(orgBotAppIDEnv))
	appSecret := strings.TrimSpace(os.Getenv(orgBotAppSecretEnv))
	if appID == "" || appSecret == "" {
		slog.Info("feishu org bot disabled (login app not configured)")
		return nil
	}
	if h == nil || h.OrgDispatchService == nil || queries == nil {
		slog.Warn("feishu org bot disabled (dispatch service unavailable)")
		return nil
	}

	creds := lark.InstallationCredentials{
		AppID:     appID,
		AppSecret: appSecret,
		Region:    lark.RegionFeishu,
	}
	client := lark.NewHTTPAPIClient(lark.HTTPClientConfig{
		BaseURL: strings.TrimSpace(os.Getenv("MULTICA_LARK_HTTP_BASE_URL")),
		Logger:  slog.Default(),
	})
	connector, err := buildOrgBotConnector(creds)
	if err != nil {
		slog.Error("feishu org bot: connector init failed", "error", err)
		return nil
	}
	return &feishuOrgBotWorker{
		queries:     queries,
		dispatcher:  h.OrgDispatchService,
		router:      h.OrgDispatchService,
		chat:        h.OrgDispatchService,
		attachments: orgBotAttachmentStore{handler: h},
		client:      client,
		connector:   connector,
		creds:       creds,
		appURL:      appURL,
		logger:      slog.Default(),
	}
}

// buildOrgBotConnector mirrors the per-agent connector with static deployment
// credentials: the organization bot has no per-installation encrypted secret.
func buildOrgBotConnector(creds lark.InstallationCredentials) (lark.EventConnector, error) {
	fetcher, err := lark.NewHTTPConnectionTokenFetcher(lark.HTTPConnectionTokenConfig{
		BaseURL: strings.TrimSpace(os.Getenv("MULTICA_LARK_CALLBACK_BASE_URL")),
		Logger:  slog.Default(),
	})
	if err != nil {
		return nil, err
	}
	dialer := lark.NewGorillaDialer()
	if proxyURL := strings.TrimSpace(os.Getenv("MULTICA_LARK_WS_PROXY_URL")); proxyURL != "" {
		dialer.ProxyURL = proxyURL
	}
	return lark.NewWSLongConnConnector(lark.WSConnectorConfig{
		Dialer:          dialer,
		EndpointFetcher: fetcher,
		FrameDecoder:    lark.NewOrgBotFrameDecoder(),
		CredentialsProvider: lark.CredentialsProviderFunc(func(context.Context, lark.Installation) (lark.InstallationCredentials, error) {
			return creds, nil
		}),
		Logger: slog.Default(),
	})
}

// Run registers the installation, then holds the lease and keeps one long
// connection alive until ctx ends.
func (w *feishuOrgBotWorker) Run(ctx context.Context) {
	ensureBackoff := orgBotMinBackoff
	for ctx.Err() == nil && w.bot == nil {
		if err := w.ensure(ctx); err != nil {
			w.logger.Warn("feishu org bot: registration failed; retrying", "error", err, "in", ensureBackoff.String())
			sleepCtx(ctx, ensureBackoff)
			ensureBackoff = min(ensureBackoff*2, orgBotMaxBackoff)
			continue
		}
	}

	backoff := orgBotMinBackoff
	for ctx.Err() == nil {
		token := uuid.NewString()
		if !w.acquire(ctx, token) {
			sleepCtx(ctx, orgBotLeasePoll)
			continue
		}

		connCtx, cancel := context.WithCancel(ctx)
		renewDone := make(chan struct{})
		go w.renew(connCtx, cancel, token, renewDone)

		started := time.Now()
		err := w.bot.Run(connCtx)
		cancel()
		<-renewDone
		w.release(token)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			w.logger.Warn("feishu org bot: connection ended", "error", err)
		}
		if time.Since(started) >= orgBotResetBackoffIn {
			backoff = orgBotMinBackoff
		} else {
			backoff = min(backoff*2, orgBotMaxBackoff)
		}
		w.logger.Info("feishu org bot: reconnecting", "in", backoff.String())
		sleepCtx(ctx, backoff)
	}
}

// ensure resolves the bot identity and persists the org installation row. The
// bot cannot run without both, and both depend on Feishu reachability.
func (w *feishuOrgBotWorker) ensure(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, orgBotEnsureTimeout)
	defer cancel()

	info, err := w.client.GetBotInfo(ctx, w.creds)
	if err != nil {
		return errors.New("bot identity lookup failed; enable the bot capability and long-connection events on the login app: " + err.Error())
	}

	// One Feishu app supports one long-connection event subscription. If the
	// login app is also bound to a per-agent bot, two readers would split the
	// same events; refuse to start rather than dispatch unpredictably.
	existing, err := w.queries.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{
		ChannelType: string(channel.TypeFeishu),
		AppID:       w.creds.AppID,
	})
	switch {
	case err == nil:
		return errors.New("the login app is already bound to a per-agent bot installation " +
			util.UUIDToString(existing.ID) + " in workspace " + util.UUIDToString(existing.WorkspaceID))
	case !errors.Is(err, pgx.ErrNoRows):
		return err
	}

	config, err := json.Marshal(map[string]string{
		"app_id": w.creds.AppID,
		"region": string(w.creds.Region),
	})
	if err != nil {
		return err
	}
	row, err := w.queries.EnsureOrgChannelInstallation(ctx, db.EnsureOrgChannelInstallationParams{
		ChannelType: lark.OrgBotChannelType,
		Config:      config,
	})
	if err != nil {
		return err
	}

	var projects lark.OrgBotProjects
	if catalog, ok := w.router.(lark.OrgBotProjects); ok {
		projects = catalog
	}
	bot, err := lark.NewOrgBot(lark.OrgBotConfig{
		InstallationID: row.ID,
		BotOpenID:      string(info.OpenID),
		Credentials:    w.creds,
		Connector:      w.connector,
		Client:         w.client,
		Identity:       w.queries,
		Organization:   w.queries,
		Dedup:          w.queries,
		Dispatcher:     w.dispatcher,
		Router:         w.router,
		Projects:       projects,
		Context:        lark.NewDBOrgBotContext(w.queries),
		Chat:           w.chat,
		Attachments:    w.attachments,
		AppURL:         w.appURL,
		Logger:         w.logger,
	})
	if err != nil {
		return err
	}
	w.bot = bot
	w.installationID = row.ID
	w.logger.Info("feishu org bot enabled", "app_id", w.creds.AppID, "bot_open_id", string(info.OpenID))
	return nil
}

func (w *feishuOrgBotWorker) acquire(ctx context.Context, token string) bool {
	_, err := w.queries.AcquireOrgChannelWSLease(ctx, db.AcquireOrgChannelWSLeaseParams{
		ID:           w.installationID,
		NewToken:     pgtype.Text{String: token, Valid: true},
		NewExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(orgBotLeaseTTL), Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	if err != nil {
		w.logger.Warn("feishu org bot: acquire lease failed", "error", err)
		return false
	}
	return true
}

func (w *feishuOrgBotWorker) renew(ctx context.Context, cancel context.CancelFunc, token string, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(orgBotLeaseRenew)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, err := w.queries.AcquireOrgChannelWSLease(ctx, db.AcquireOrgChannelWSLeaseParams{
				ID:           w.installationID,
				NewToken:     pgtype.Text{String: token, Valid: true},
				NewExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(orgBotLeaseTTL), Valid: true},
			})
			if err != nil {
				// Lost ownership (or a persisted DB error): drop the connection
				// before another replica can take the lease.
				w.logger.Warn("feishu org bot: lease renewal failed; reconnecting", "error", err)
				cancel()
				return
			}
		}
	}
}

func (w *feishuOrgBotWorker) release(token string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.queries.ReleaseOrgChannelWSLease(ctx, db.ReleaseOrgChannelWSLeaseParams{
		ID:           w.installationID,
		CurrentToken: pgtype.Text{String: token, Valid: true},
	}); err != nil {
		w.logger.Warn("feishu org bot: release lease failed", "error", err)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

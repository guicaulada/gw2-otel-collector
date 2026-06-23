// Package poller fetches each endpoint family on its own interval and writes
// the result into the store. Each family runs in its own goroutine so intervals
// are independent; all stop when the context is cancelled.
package poller

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/guicaulada/gw2-otel-collector/internal/config"
	"github.com/guicaulada/gw2-otel-collector/internal/gw2"
	"github.com/guicaulada/gw2-otel-collector/internal/store"
)

// Fixed quantities used to price the gem/coin exchange, so the rate is
// comparable over time (small inputs return degenerate rates).
const (
	exchangeCoinsQuantity = 100000 // 10 gold -> price of a gem in coins
	exchangeGemsQuantity  = 100    // 100 gems -> coins received per gem
)

// Poller drives scheduled polling of the GW2 API.
type Poller struct {
	client    *gw2.Client
	store     *store.Store
	intervals config.Intervals
	log       *slog.Logger
	wg        sync.WaitGroup
}

// New returns a Poller.
func New(client *gw2.Client, st *store.Store, intervals config.Intervals, log *slog.Logger) *Poller {
	return &Poller{client: client, store: st, intervals: intervals, log: log}
}

// Start launches one goroutine per family. It returns immediately; call Wait to
// block until all goroutines have stopped after the context is cancelled.
func (p *Poller) Start(ctx context.Context) {
	p.run(ctx, "account", p.intervals.Account, func(ctx context.Context) error {
		a, err := p.client.Account(ctx)
		if err != nil {
			return err
		}
		p.store.SetAccount(a, time.Now())
		return nil
	})

	p.run(ctx, "wallet", p.intervals.Wallet, func(ctx context.Context) error {
		w, err := p.client.Wallet(ctx)
		if err != nil {
			return err
		}
		p.store.SetWallet(w, time.Now())
		return nil
	})

	p.run(ctx, "characters", p.intervals.Characters, func(ctx context.Context) error {
		ch, err := p.client.Characters(ctx)
		if err != nil {
			return err
		}
		p.store.SetCharacters(ch, time.Now())
		return nil
	})

	p.run(ctx, "commerce", p.intervals.Commerce, func(ctx context.Context) error {
		buy, err := p.client.ExchangeCoins(ctx, exchangeCoinsQuantity)
		if err != nil {
			return err
		}
		sell, err := p.client.ExchangeGems(ctx, exchangeGemsQuantity)
		if err != nil {
			return err
		}
		delivery, err := p.client.Delivery(ctx)
		if err != nil {
			return err
		}
		p.store.SetCommerce(&store.Commerce{
			CoinsPerGemBuy:  buy.CoinsPerGem,
			CoinsPerGemSell: sell.CoinsPerGem,
			DeliveryCoins:   delivery.Coins,
			DeliveryItems:   int64(len(delivery.Items)),
		}, time.Now())
		return nil
	})
}

// Wait blocks until all polling goroutines have exited.
func (p *Poller) Wait() { p.wg.Wait() }

// run polls once immediately, then on every tick until the context is cancelled.
func (p *Poller) run(ctx context.Context, family string, interval time.Duration, fetch func(context.Context) error) {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		log := p.log.With("family", family, "interval", interval.String())

		poll := func() {
			if err := fetch(ctx); err != nil {
				if ctx.Err() == nil {
					log.Error("poll failed", "error", err)
				}
				return
			}
			log.Debug("poll ok")
		}

		poll() // initial fetch so metrics are populated quickly

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Debug("poller stopping")
				return
			case <-ticker.C:
				poll()
			}
		}
	}()
}

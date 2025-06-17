package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

var DefaultRefreshInterval = 12 * time.Hour

const modelPricingURL = "https://raw.githubusercontent.com/BerriAI/litellm/refs/heads/main/model_prices_and_context_window.json"

type ModelPrice struct {
	OutputCostPerToken float64 `json:"output_cost_per_token"`
	InputCostPerToken  float64 `json:"input_cost_per_token"`
	Provider           string  `json:"litellm_provider"`
	Mode               string  `json:"mode"` // chat, embedding, moderation, audio_speech, audio_transcription, etc
}

// ModelPricing is an interface for getting the price for a given model
type ModelPricing interface {
	// Start starts the pricing update loop
	Start(ctx context.Context)
	// GetPrice returns the price for a given model, or nil if the model is not found
	GetPrice(model string) *ModelPrice
	// Close stops the pricing update loop
	Close()
}

type modelPricing struct {
	pricing     map[string]*ModelPrice
	lastUpdated time.Time
	mu          sync.RWMutex
	ctx         context.Context
	cancelFunc  context.CancelFunc
	once        sync.Once
	onError     func(error)
	onUpdate    func(int)
	interval    time.Duration
}

func (p *modelPricing) GetPrice(model string) *ModelPrice {
	p.mu.RLock()
	val := p.pricing[model]
	p.mu.RUnlock()
	return val
}

func (p *modelPricing) Close() {
	p.once.Do(func() {
		if p.cancelFunc != nil {
			p.cancelFunc()
		}
	})
}

func (p *modelPricing) updatePrices() {
	req, err := http.NewRequestWithContext(p.ctx, "GET", modelPricingURL, nil)
	if err != nil {
		p.onError(fmt.Errorf("failed to create request: %w", err))
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		p.onError(fmt.Errorf("failed to update prices: %w", err))
		return
	}
	defer resp.Body.Close()

	var pricing map[string]*ModelPrice
	if err = json.NewDecoder(resp.Body).Decode(&pricing); err != nil {
		p.onError(fmt.Errorf("failed to decode pricing: %w", err))
		return
	}
	p.mu.Lock()
	p.pricing = pricing
	p.lastUpdated = time.Now()
	p.onUpdate(len(pricing))
	p.mu.Unlock()
}

func (p *modelPricing) run() {
	t := time.NewTicker(p.interval)
	defer t.Stop()

	p.updatePrices()

	for {
		select {
		case <-t.C:
			p.updatePrices()
		case <-p.ctx.Done():
			return
		}
	}
}

type ModelPricingOption func(*modelPricing)

func WithInterval(interval time.Duration) ModelPricingOption {
	return func(p *modelPricing) {
		p.interval = interval
	}
}

func WithOnError(onError func(error)) ModelPricingOption {
	return func(p *modelPricing) {
		p.onError = onError
	}
}

func WithOnUpdate(onUpdate func(int)) ModelPricingOption {
	return func(p *modelPricing) {
		p.onUpdate = onUpdate
	}
}

func NewModelPricing(options ...ModelPricingOption) ModelPricing {
	lm := &modelPricing{}

	for _, option := range options {
		option(lm)
	}
	if lm.onError == nil {
		lm.onError = func(err error) {
			fmt.Printf("error updating LLM pricing: %s", err)
		}
	}
	if lm.onUpdate == nil {
		lm.onUpdate = func(count int) {} // no-op
	}
	if lm.interval == 0 {
		lm.interval = DefaultRefreshInterval
	}

	return lm
}

func (p *modelPricing) Start(ctx context.Context) {
	p.ctx, p.cancelFunc = context.WithCancel(ctx)
	go p.run()
}

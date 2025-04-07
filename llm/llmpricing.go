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

type ModelPricing struct {
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

func (p *ModelPricing) GetPrice(model string) *ModelPrice {
	p.mu.RLock()
	val := p.pricing[model]
	p.mu.RUnlock()
	return val
}

func (p *ModelPricing) Close() {
	p.once.Do(func() {
		p.cancelFunc()
	})
}

func (p *ModelPricing) updatePrices() {
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

func (p *ModelPricing) run() {
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

type LLMModelPricingOption func(*ModelPricing)

func WithInterval(interval time.Duration) LLMModelPricingOption {
	return func(p *ModelPricing) {
		p.interval = interval
	}
}

func WithOnError(onError func(error)) LLMModelPricingOption {
	return func(p *ModelPricing) {
		p.onError = onError
	}
}

func WithOnUpdate(onUpdate func(int)) LLMModelPricingOption {
	return func(p *ModelPricing) {
		p.onUpdate = onUpdate
	}
}

func NewLLMModelPricing(ctx context.Context, options ...LLMModelPricingOption) *ModelPricing {
	ctx, cancel := context.WithCancel(ctx)
	lm := &ModelPricing{
		ctx:        ctx,
		cancelFunc: cancel,
	}

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

	go lm.run()
	return lm
}

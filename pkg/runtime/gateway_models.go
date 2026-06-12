package runtime

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/modelsdev"
	"github.com/docker/docker-agent/pkg/modelsgateway"
)

// gatewayModelsTTL is how long a gateway /v1/models response (or failure)
// is reused before the gateway is queried again. It keeps the model picker
// snappy on repeated opens while still picking up gateway changes
// eventually.
const gatewayModelsTTL = 5 * time.Minute

// gatewayModelsCache memoizes the result of the gateway model discovery,
// including failures, so an unsupported or slow gateway is not re-queried
// on every picker open.
type gatewayModelsCache struct {
	mu        sync.Mutex
	ids       []string
	err       error
	fetchedAt time.Time
}

// listGatewayModels returns the model IDs served by the configured models
// gateway, using the runtime's cache when fresh.
func (r *LocalRuntime) listGatewayModels(ctx context.Context) ([]string, error) {
	now := time.Now
	if r.now != nil {
		now = r.now
	}

	c := &r.gatewayModels
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.fetchedAt.IsZero() && now().Sub(c.fetchedAt) < gatewayModelsTTL {
		return c.ids, c.err
	}

	c.ids, c.err = modelsgateway.ListModels(ctx, r.modelSwitcherCfg.ModelsGateway, r.modelSwitcherCfg.EnvProvider)
	c.fetchedAt = now()
	return c.ids, c.err
}

// buildGatewayChoices builds ModelChoice entries from the models served by
// the configured gateway, deduplicated against the explicitly configured
// models. The second return value reports whether discovery succeeded;
// when false (gateway unreachable, /v1/models unsupported, or an empty
// list that gives no usable signal) callers should fall back to the
// models.dev catalog.
func (r *LocalRuntime) buildGatewayChoices(ctx context.Context) ([]ModelChoice, bool) {
	ids, err := r.listGatewayModels(ctx)
	if err != nil {
		slog.DebugContext(ctx, "Gateway model discovery failed, falling back to catalog", "error", err)
		return nil, false
	}
	if len(ids) == 0 {
		slog.DebugContext(ctx, "Gateway returned no models, falling back to catalog")
		return nil, false
	}

	existingRefs := make(map[string]bool, len(r.modelSwitcherCfg.Models)*2)
	for name, cfg := range r.modelSwitcherCfg.Models {
		existingRefs[name] = true
		if cfg.Provider != "" && cfg.Model != "" {
			existingRefs[cfg.Provider+"/"+cfg.Model] = true
		}
	}

	choices := make([]ModelChoice, 0, len(ids))
	for _, id := range ids {
		prov, model, ok := strings.Cut(id, "/")
		if !ok {
			// Bare IDs (no provider prefix) are served through the
			// gateway's OpenAI-compatible endpoint, so route them
			// through the openai provider.
			prov, model = "openai", id
		}
		if _, err := latest.ParseModelRef(prov + "/" + model); err != nil {
			continue
		}
		if isEmbeddingModel("", model) {
			continue
		}

		ref := prov + "/" + model
		if existingRefs[ref] {
			continue
		}
		existingRefs[ref] = true

		choice := ModelChoice{
			Name:      model,
			Ref:       ref,
			Provider:  prov,
			Model:     model,
			IsCatalog: true,
		}
		if r.modelsStore != nil {
			if m, err := r.modelsStore.GetModel(ctx, modelsdev.NewID(prov, model)); err == nil && m != nil {
				if m.Name != "" {
					choice.Name = m.Name
				}
				applyCatalogMetadata(&choice, m)
			}
		}
		choices = append(choices, choice)
	}

	slog.DebugContext(ctx, "Built gateway model choices", "count", len(choices))
	return choices, true
}

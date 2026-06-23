package agents

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/flow"
	"github.com/curtisnewbie/miso/util/hash"
)

const modelsDevURL = "https://models.dev/api.json"

type modelsDevEntry struct {
	ID    string `json:"id"`
	Limit struct {
		Context int `json:"context"`
	} `json:"limit"`
}

type modelsDevProvider struct {
	Models map[string]modelsDevEntry `json:"models"`
}

var modelCache = hash.NewStrRWMap[int]()

var modelsDevClient = &http.Client{Timeout: 5 * time.Second}

// LookupModelContextWindow returns the context window size for a model.
// First checks the build-time generated map (ModelContextWindow), then the
// process-level in-memory cache. If not found and enableFetch is true,
// fetches from models.dev and populates the cache.
func LookupModelContextWindow(rail flow.Rail, modelName string, enableFetch bool) (int, bool) {
	if n, ok := ModelContextWindow[modelName]; ok {
		return n, true
	}
	if n, ok := modelCache.Get(modelName); ok {
		return n, true
	}
	if !enableFetch {
		return 0, false
	}
	fetched, err := fetchModelContextWindows()
	if err != nil {
		rail.Errorf("models_fetch: failed to fetch model context windows: %v", err)
		return 0, false
	}
	for k, v := range fetched {
		modelCache.Put(k, v)
	}
	n, ok := fetched[modelName]
	return n, ok
}

func fetchModelContextWindows() (map[string]int, error) {
	resp, err := modelsDevClient.Get(modelsDevURL) //nolint:gosec
	if err != nil {
		return nil, errs.Wrap(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errs.NewErrf("models.dev returned status %d", resp.StatusCode)
	}

	var providers map[string]modelsDevProvider
	if err := json.NewDecoder(resp.Body).Decode(&providers); err != nil {
		return nil, errs.Wrap(err)
	}

	result := make(map[string]int)
	for _, p := range providers {
		for _, m := range p.Models {
			if m.ID != "" && m.Limit.Context > 0 {
				result[m.ID] = m.Limit.Context
			}
		}
	}
	return result, nil
}

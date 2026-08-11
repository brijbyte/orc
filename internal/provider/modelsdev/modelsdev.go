// Package modelsdev lists provider models from https://models.dev,
// cached in <orc home>/models-dev.json for a day; best effort.
package modelsdev

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/brijbyte/orc/internal/config"
	"github.com/brijbyte/orc/internal/provider"
)

const apiURL = "https://models.dev/api.json"
const ttl = 24 * time.Hour

// cacheVersion invalidates cache files whose Model shape predates ours.
const cacheVersion = 3

type cache struct {
	Version   int                         `json:"v"`
	Fetched   time.Time                   `json:"fetched"`
	Providers map[string][]provider.Model `json:"providers"`
}

// Models returns the tool-capable text models for a models.dev provider id,
// newest first. Stale cache is served when the network fails; nil otherwise.
func Models(providerID string) []provider.Model {
	c := load()
	if c == nil {
		return nil
	}
	return c.Providers[providerID]
}

// load returns the cache, refetching when missing or older than ttl.
func load() *cache {
	path := config.Path("models-dev.json")
	c := read(path)
	if c == nil || time.Since(c.Fetched) > ttl {
		if fresh := fetch(); fresh != nil {
			c = fresh
			if data, err := json.Marshal(c); err == nil {
				config.WriteFileAtomic(path, data)
			}
		}
	}
	return c
}

// StartDailyRefresh keeps the cache fresh for the life of the process.
func StartDailyRefresh() {
	go func() {
		for {
			load()
			time.Sleep(ttl / 24)
		}
	}()
}

func read(path string) *cache {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var c cache
	if json.Unmarshal(data, &c) != nil || c.Providers == nil ||
		c.Version != cacheVersion {
		return nil
	}
	return &c
}

func fetch() *cache {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var api map[string]struct {
		Models map[string]struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			ToolCall    bool   `json:"tool_call"`
			ReleaseDate string `json:"release_date"`
			Reasoning   []struct {
				Type   string   `json:"type"`
				Values []string `json:"values"`
			} `json:"reasoning_options"`
			Modalities struct {
				Output []string `json:"output"`
			} `json:"modalities"`
			Limit struct {
				Context int64 `json:"context"`
			} `json:"limit"`
		} `json:"models"`
	}
	if json.NewDecoder(resp.Body).Decode(&api) != nil {
		return nil
	}
	c := &cache{Version: cacheVersion, Fetched: time.Now(),
		Providers: map[string][]provider.Model{}}
	for id, prov := range api {
		var out []provider.Model
		release := map[string]string{}
		for slug, m := range prov.Models {
			if slug == "" || !m.ToolCall || !emitsText(m.Modalities.Output) {
				continue
			}
			var efforts []string
			for _, r := range m.Reasoning {
				if r.Type == "effort" {
					efforts = r.Values
				}
			}
			out = append(out, provider.Model{Slug: slug, Name: m.Name,
				Description: m.Description, Efforts: efforts,
				ContextWindow: m.Limit.Context})
			release[slug] = m.ReleaseDate
		}
		sort.Slice(out, func(i, j int) bool {
			if a, b := release[out[i].Slug], release[out[j].Slug]; a != b {
				return a > b
			}
			return out[i].Slug < out[j].Slug
		})
		c.Providers[id] = out
	}
	return c
}

func emitsText(output []string) bool {
	for _, mode := range output {
		if mode == "text" {
			return true
		}
	}
	return false
}

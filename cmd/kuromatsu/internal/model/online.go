package model

import (
	"context"

	"github.com/andre25costa-code/kuromatsu/pkg/setup"
)

type modelEntry = setup.ModelEntry

// fetchOpenAIModels GETs <baseURL>/models with Bearer auth and accepts both the
// {data:[…]} envelope and a bare array shape used by various OpenAI-compatible servers.
func fetchOpenAIModels(baseURL, apiKey string) ([]modelEntry, error) {
	return setup.ProbeModels(context.Background(), setup.KindOpenAI, baseURL, apiKey)
}

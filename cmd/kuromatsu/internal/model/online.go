package model

import (
	"context"

	"github.com/andre25costa-code/kuromatsu/pkg/setup"
)

type modelEntry = setup.ModelEntry

// fetchModels GETs <baseURL>/models with the auth of the probe kind
// (setup.Kind*) and accepts both the {data:[…]} envelope and a bare array
// shape used by various OpenAI-compatible servers.
func fetchModels(kind, baseURL, apiKey string) ([]modelEntry, error) {
	return setup.ProbeModels(context.Background(), kind, baseURL, apiKey)
}

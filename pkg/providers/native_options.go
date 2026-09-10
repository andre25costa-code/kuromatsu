package providers

import (
	"fmt"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/providers/common"
	"github.com/andre25costa-code/kuromatsu/pkg/providers/localllm"
)

// nativeOptionsFromModelConfig resolves localllm.Options for the "native"
// protocol: the GGUF path via localllm.ResolveModelPath, plus any per-model
// runtime knobs carried in ExtraBody (ADR-002). Unset ExtraBody keys keep
// localllm's own defaults (Options.WithDefaults).
func nativeOptionsFromModelConfig(cfg *config.ModelConfig, modelID string) (localllm.Options, error) {
	path, err := localllm.ResolveModelPath(modelID, config.GetHome())
	if err != nil {
		return localllm.Options{}, fmt.Errorf("native provider: %w", err)
	}

	opts := localllm.Options{ModelPath: path}
	if cfg.ExtraBody != nil {
		if v, ok := common.AsInt(cfg.ExtraBody["n_ctx"]); ok {
			opts.NCtx = v
		}
		if v, ok := common.AsInt(cfg.ExtraBody["n_threads"]); ok {
			opts.NThreads = v
		}
		if v, ok := common.AsInt(cfg.ExtraBody["n_batch"]); ok {
			opts.NBatch = v
		}
		if v, ok := cfg.ExtraBody["kv_cache_type"].(string); ok {
			opts.KVCacheType = v
		}
		if v, ok := common.AsInt(cfg.ExtraBody["keep_alive_secs"]); ok {
			opts.KeepAliveSecs = v
		}
		if v, ok := common.AsInt(cfg.ExtraBody["max_predict"]); ok {
			opts.MaxPredict = v
		}
	}
	return opts, nil
}

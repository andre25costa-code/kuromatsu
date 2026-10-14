package channels

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

func toChannelHashes(cfg *config.Config) map[string]string {
	result := make(map[string]string)
	ch := cfg.Channels
	marshal, err := json.Marshal(ch)
	if err != nil {
		logger.ErrorCF("channels", "failed to marshal channels config", map[string]any{"error": err.Error()})
		return result
	}
	var channelConfig map[string]map[string]any
	if err := json.Unmarshal(marshal, &channelConfig); err != nil {
		logger.ErrorCF("channels", "failed to unmarshal channels config", map[string]any{"error": err.Error()})
		return result
	}

	for key, value := range channelConfig {
		if enabled, ok := value["enabled"].(bool); !ok || !enabled {
			continue
		}
		hiddenValues(key, value, ch.Get(key))
		valueBytes, err := json.Marshal(value)
		if err != nil {
			logger.ErrorCF("channels", "failed to marshal channel config", map[string]any{
				"channel": key, "error": err.Error(),
			})
			continue
		}
		hash := md5.Sum(valueBytes)
		result[key] = hex.EncodeToString(hash[:])
	}

	return result
}

func hiddenValues(key string, value map[string]any, ch *config.Channel) {
	v, err := ch.GetDecoded()
	if err != nil {
		return
	}
	switch key {
	case "pico":
		if settings, ok := v.(*config.PicoSettings); ok {
			value["token"] = settings.Token.String()
		}
	case "telegram":
		if settings, ok := v.(*config.TelegramSettings); ok {
			value["token"] = settings.Token.String()
		}
	}
}

func compareChannels(old, news map[string]string) (added, removed []string) {
	for key, newHash := range news {
		if oldHash, ok := old[key]; ok {
			if newHash != oldHash {
				removed = append(removed, key)
				added = append(added, key)
			}
		} else {
			added = append(added, key)
		}
	}
	for key := range old {
		if _, ok := news[key]; !ok {
			removed = append(removed, key)
		}
	}
	return added, removed
}

func toChannelConfig(cfg *config.Config, list []string) (*config.ChannelsConfig, error) {
	result := make(config.ChannelsConfig)
	for _, name := range list {
		bc, ok := cfg.Channels[name]
		if !ok || !bc.Enabled {
			continue
		}
		result[name] = bc
	}
	return &result, nil
}

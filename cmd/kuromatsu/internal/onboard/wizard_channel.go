package onboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

// --- step 3: channel ----------------------------------------------------

// telegramAPIBase is the Telegram Bot API root (a seam for tests).
var telegramAPIBase = "https://api.telegram.org"

// getMeTimeout bounds the optional Telegram bot check.
const getMeTimeout = 10 * time.Second

var (
	telegramTokenRe = regexp.MustCompile(`^\d+:[A-Za-z0-9_-]{30,}$`)
	digitsRe        = regexp.MustCompile(`^\d+$`)
)

func (w *wizard) stepChannel() error {
	tg := w.cfg.Channels[config.ChannelTelegram]
	configured := tg != nil && tg.Enabled && len(tg.AllowFrom) > 0
	keep, def := "", 2
	if configured {
		keep, def = "Telegram, allow_from "+strings.Join(tg.AllowFrom, ","), 0
	}
	n, err := w.choose(w.msg.channelTitle, keep, w.msg.channelOptions, def)
	if err != nil {
		return err
	}
	switch n {
	case 0:
		w.sumChannel, w.hasChannel = w.msg.sumChannelKeep, true
		return nil
	case 2:
		// "None" never touches channel_list: a channel that is already
		// enabled stays so, and the gateway is still the next command.
		w.sumChannel = w.msg.sumChannelNone
		if tg != nil && tg.Enabled {
			w.sumChannel, w.hasChannel = w.msg.sumChannelKeep, true
		}
		return nil
	}

	if tg == nil {
		tg = &config.Channel{Type: config.ChannelTelegram}
		tg.SetName(config.ChannelTelegram)
		w.cfg.Channels[config.ChannelTelegram] = tg
	}
	if tg.SettingsIsEmpty() {
		tg.Settings = config.RawNode("{}")
	}
	tg.Type = config.ChannelTelegram
	decoded, err := tg.GetDecoded()
	if err != nil {
		return err
	}
	settings, ok := decoded.(*config.TelegramSettings)
	if !ok {
		return fmt.Errorf("telegram channel settings have an unexpected type %T", decoded)
	}

	current := settings.Token.String()
	var token string
	for token == "" {
		prompt := w.msg.tgToken
		if current != "" {
			prompt = w.msg.tgTokenKeep
		}
		if token, err = w.secret(prompt); err != nil {
			return err
		}
		if token == "" && current != "" {
			token = current
			break
		}
		if !telegramTokenRe.MatchString(token) {
			w.println(w.msg.tgTokenInvalid)
			token = ""
		}
	}

	ownerDef := ""
	if len(tg.AllowFrom) > 0 {
		ownerDef = strings.Join(tg.AllowFrom, ",")
	}
	var owners []string
	for owners == nil {
		text, err := w.ask(w.msg.tgOwner, ownerDef)
		if err != nil {
			return err
		}
		if text != "" && text == ownerDef {
			owners = append([]string(nil), tg.AllowFrom...)
			break
		}
		if !digitsRe.MatchString(text) {
			w.println(w.msg.tgOwnerInvalid)
			continue
		}
		owners = []string{text}
	}

	w.checkTelegram(token)

	tg.Enabled = true
	tg.AllowFrom = config.FlexibleStringSlice(owners)
	if token != current {
		settings.Token.Set(token)
	}
	w.sumChannel = fmt.Sprintf(w.msg.sumTelegram, strings.Join(owners, ","), maskKey(token))
	w.hasChannel = true
	return nil
}

// checkTelegram calls getMe and only warns on failure: the network may
// block the API. Errors are printed without the token (it is in the URL).
func (w *wizard) checkTelegram(token string) {
	ctx, cancel := context.WithTimeout(context.Background(), getMeTimeout)
	defer cancel()
	username, err := telegramGetMe(ctx, token)
	if err != nil {
		w.printf(w.msg.getMeFailed+"\n", maskSecret(err.Error(), token))
		return
	}
	w.printf(w.msg.getMeOK+"\n", username)
}

func telegramGetMe(ctx context.Context, token string) (string, error) {
	url := strings.TrimRight(telegramAPIBase, "/") + "/bot" + token + "/getMe"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var body struct {
		OK     bool `json:"ok"`
		Result struct {
			Username string `json:"username"`
		} `json:"result"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body); err != nil {
		return "", fmt.Errorf("HTTP %d: %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK || !body.OK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, body.Description)
	}
	return body.Result.Username, nil
}

// maskSecret removes secret from s, then masks anything else that looks
// like a credential.
func maskSecret(s, secret string) string {
	if secret != "" {
		s = strings.ReplaceAll(s, secret, "****")
	}
	return logger.MaskSecrets(s)
}

// maskKey shows a key as its first 3 and last 4 characters (sk-****abcd).
func maskKey(k string) string {
	if len(k) <= 8 {
		return "****"
	}
	return k[:3] + "****" + k[len(k)-4:]
}

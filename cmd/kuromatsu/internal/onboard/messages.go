package onboard

// Languages offered by the onboard wizard.
const (
	langPtBR = "pt-BR"
	langEn   = "en"
)

// messages is the text of the onboard wizard in one language. Strings with
// verbs are fmt formats.
type messages struct {
	// userLanguageLine is appended to the workspace USER.md, which the agent
	// reads, so replies follow the chosen language.
	userLanguageLine string

	intro         string
	invalidChoice string
	keepCurrent   string // %s: what is kept
	quitHint      string

	providerTitle   string
	providerOptions []string
	baseURL         string
	baseRequired    string
	apiKey          string
	apiKeyKeep      string
	apiKeyRequired  string
	secretVisible   string
	probing         string // %s: URL
	probeOK         string // %d: model count
	probeEmpty      string
	probeFailed     string // %s: error
	probeMenu       string
	manualModel     string

	channelTitle         string
	channelOptions       []string
	tgToken              string
	tgTokenKeep          string
	tgTokenInvalid       string
	tgOwner              string
	tgOwnerInvalid       string
	getMeOK              string // %s: bot username
	getMeFailed          string // %s: error
	hardwareTitle        string
	hardwareLine         string // %s RAM, %d CPUs, %s detected profile
	hardwareUse          string // %s: profile
	timezonePrompt       string
	timezonePromptSystem string // when no zone is known: Enter keeps the system zone
	timezoneInvalid      string // %s: error

	permTitle   string
	permOptions []string

	summaryTitle      string
	sumModel          string // %s alias, %s provider, %s model, %s base, %s key
	sumModelKeep      string
	sumTelegram       string // %s allow_from, %s token
	sumChannelKeep    string
	sumChannelNone    string
	sumProfile        string // %s
	sumProfileKeep    string
	sumTimezone       string // %s
	sumTimezoneSystem string
	sumPermissions    string // %s
	sumPermKeep       string // %s
	sumLanguage       string // %s
	confirm           string
	aborted           string
	saved             string // %s: config path
	nextCommand       string // %s: command
	warning           string // %s
	providerOpenAIC   string
}

var messageTable = map[string]*messages{langPtBR: msgPtBR, langEn: msgEn}

//nolint:dupl // the English table below has the same keys
var msgPtBR = &messages{
	userLanguageLine: "- Idioma preferido: pt-BR",

	intro:         "Configuração inicial do Kuromatsu. Enter aceita o valor entre colchetes; q sai sem gravar nada.",
	invalidChoice: "Opção inválida.",
	keepCurrent:   "Manter atual (%s)",
	quitHint:      "Saindo sem gravar nada.",

	providerTitle: "Provedor de modelo",
	providerOptions: []string{
		"OpenAI-compatível", "Ollama local", "Anthropic", "Gemini", "OpenRouter", "Pular",
	},
	baseURL:        "URL base da API",
	baseRequired:   "A URL base é obrigatória.",
	apiKey:         "Chave da API (oculta): ",
	apiKeyKeep:     "Chave da API (oculta; Enter mantém a atual): ",
	apiKeyRequired: "A chave é obrigatória para este provedor.",
	secretVisible:  "Não foi possível ocultar a digitação; input will be visible (a entrada ficará visível): ",
	probing:        "Testando a conexão com %s ...",
	probeOK:        "Conexão ok: %d modelo(s).",
	probeEmpty:     "o servidor não listou nenhum modelo",
	probeFailed:    "Teste de conexão falhou: %s",
	probeMenu:      "[r]etentar  [e]ditar  [s]alvar mesmo assim  [p]ular",
	manualModel:    "ID do modelo",

	channelTitle:   "Canal",
	channelOptions: []string{"Telegram", "Nenhum (só CLI)"},
	tgToken:        "Token do bot do Telegram (oculto): ",
	tgTokenKeep:    "Token do bot do Telegram (oculto; Enter mantém o atual): ",
	tgTokenInvalid: "Token inválido (formato esperado: 123456:ABC..., com 30 ou mais caracteres depois dos dois-pontos).",
	tgOwner:        "Seu ID numérico do Telegram (fale com @userinfobot para descobrir)",
	tgOwnerInvalid: "Informe só dígitos; sem o ID, o bot rejeita todas as mensagens.",
	getMeOK:        "Bot verificado: @%s",
	getMeFailed:    "Aviso: não foi possível verificar o bot (%s). O token foi mantido; confira a rede depois.",

	hardwareTitle:        "Perfil de hardware",
	hardwareLine:         "Hardware: %s de RAM, %d CPUs → perfil detectado %s",
	hardwareUse:          "usar %s",
	timezonePrompt:       "Fuso horário (IANA, ex.: America/Sao_Paulo)",
	timezonePromptSystem: "Fuso horário (IANA, ex.: America/Sao_Paulo; Enter usa o fuso do sistema)",
	timezoneInvalid:      "Fuso inválido: %s",

	permTitle: "Permissões",
	permOptions: []string{
		"Seguro (sem exec, web e cron)",
		"Assistente (web e lembretes; sem exec)",
		"Avançado (exec só em canais internos; o cron pode rodar shell)",
	},

	summaryTitle:      "Resumo do que será gravado:",
	sumModel:          "  Modelo: %s (provedor %s) → %s em %s, chave %s",
	sumModelKeep:      "  Modelo: sem alteração",
	sumTelegram:       "  Canal: Telegram, allow_from %s, token %s",
	sumChannelKeep:    "  Canal: sem alteração",
	sumChannelNone:    "  Canal: nenhum (só CLI)",
	sumProfile:        "  Perfil de hardware: %s",
	sumProfileKeep:    "  Perfil de hardware: sem alteração",
	sumTimezone:       "  Fuso horário: %s",
	sumTimezoneSystem: "  Fuso horário: o do sistema (timezone vazio)",
	sumPermissions:    "  Permissões: %s",
	sumPermKeep:       "  Permissões: sem alteração (%s)",
	sumLanguage:       "  Idioma: %s (linha no USER.md)",
	confirm:           "Gravar? [S/n]: ",
	aborted:           "Cancelado; nada foi gravado.",
	saved:             "Configuração gravada em %s",
	nextCommand:       "Próximo passo: %s",
	warning:           "Aviso: %s",
	providerOpenAIC:   "compatível com OpenAI",
}

//nolint:dupl // the Portuguese table above has the same keys
var msgEn = &messages{
	userLanguageLine: "- Preferred language: en",

	intro:         "Kuromatsu first-time setup. Enter accepts the value in brackets; q quits without saving anything.",
	invalidChoice: "Invalid choice.",
	keepCurrent:   "Keep current (%s)",
	quitHint:      "Quitting without saving anything.",

	providerTitle: "Model provider",
	providerOptions: []string{
		"OpenAI-compatible", "Local Ollama", "Anthropic", "Gemini", "OpenRouter", "Skip",
	},
	baseURL:        "API base URL",
	baseRequired:   "The base URL is required.",
	apiKey:         "API key (hidden): ",
	apiKeyKeep:     "API key (hidden; Enter keeps the current one): ",
	apiKeyRequired: "This provider needs a key.",
	secretVisible:  "Could not hide typing; input will be visible: ",
	probing:        "Testing the connection to %s ...",
	probeOK:        "Connection ok: %d model(s).",
	probeEmpty:     "the server listed no models",
	probeFailed:    "Connection test failed: %s",
	probeMenu:      "[r]etry  [e]dit  [s]ave anyway  [p]skip",
	manualModel:    "Model id",

	channelTitle:   "Channel",
	channelOptions: []string{"Telegram", "None (CLI only)"},
	tgToken:        "Telegram bot token (hidden): ",
	tgTokenKeep:    "Telegram bot token (hidden; Enter keeps the current one): ",
	tgTokenInvalid: "Invalid token (expected 123456:ABC..., with 30 or more characters after the colon).",
	tgOwner:        "Your numeric Telegram ID (ask @userinfobot to find it)",
	tgOwnerInvalid: "Digits only; without the ID the bot rejects every message.",
	getMeOK:        "Bot verified: @%s",
	getMeFailed:    "Warning: could not verify the bot (%s). The token is kept; check the network later.",

	hardwareTitle:        "Hardware profile",
	hardwareLine:         "Hardware: %s RAM, %d CPUs → detected profile %s",
	hardwareUse:          "use %s",
	timezonePrompt:       "Timezone (IANA, e.g. Europe/London)",
	timezonePromptSystem: "Timezone (IANA, e.g. Europe/London; Enter uses the system zone)",
	timezoneInvalid:      "Invalid timezone: %s",

	permTitle: "Permissions",
	permOptions: []string{
		"Safe (no exec, web or cron)",
		"Assistant (web and reminders; no exec)",
		"Advanced (exec on internal channels only, cron with commands)",
	},

	summaryTitle:      "Summary of what will be saved:",
	sumModel:          "  Model: %s (provider %s) → %s at %s, key %s",
	sumModelKeep:      "  Model: unchanged",
	sumTelegram:       "  Channel: Telegram, allow_from %s, token %s",
	sumChannelKeep:    "  Channel: unchanged",
	sumChannelNone:    "  Channel: none (CLI only)",
	sumProfile:        "  Hardware profile: %s",
	sumProfileKeep:    "  Hardware profile: unchanged",
	sumTimezone:       "  Timezone: %s",
	sumTimezoneSystem: "  Timezone: the system zone (timezone left empty)",
	sumPermissions:    "  Permissions: %s",
	sumPermKeep:       "  Permissions: unchanged (%s)",
	sumLanguage:       "  Language: %s (line in USER.md)",
	confirm:           "Save? [Y/n]: ",
	aborted:           "Canceled; nothing was saved.",
	saved:             "Config saved to %s",
	nextCommand:       "Next: %s",
	warning:           "Warning: %s",
	providerOpenAIC:   "OpenAI-compatible",
}

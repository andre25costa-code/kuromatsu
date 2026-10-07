# Voz — transcrição (ASR) e síntese de fala (TTS)

Fonte real: `pkg/audio/asr/`, `pkg/audio/tts/`.

## Modelo mental

O Kuromatsu não guarda chave de API de voz dentro do bloco `voice`. Em vez
disso:

1. Adiciona-se uma entrada ASR/TTS-capable em `model_list`.
2. `voice.model_name` (ASR) e/ou `voice.tts_model_name` (TTS) apontam para o
   nome dessa entrada.
3. A chave de API mora em `.security.yml`, sob o mesmo nome do `model_list`.

Esse é o mesmo padrão usado para os outros modelos LLM — nada especial de
voz.

## ASR (fala → texto)

Recomendação rápida: `groq/whisper-large-v3-turbo` (rápido, tier free de
2000 req/dia hoje) ou `elevenlabs/scribe_v1` (bom, tier free inclui STT).
Preços/limites de free tier mudam — checar a página do provider antes de
depender disso em produção.

```json
{
  "voice": { "model_name": "groq-asr", "echo_transcription": true },
  "model_list": [{ "model_name": "groq-asr", "model": "groq/whisper-large-v3-turbo" }]
}
```
```yaml
# .security.yml
model_list:
  groq-asr:
    api_keys: ["gsk_sua_chave"]
```

Três rotas de ASR existem hoje:

| Rota | Exemplos de modelo | Comportamento |
|---|---|---|
| ElevenLabs ASR | `provider: elevenlabs`, `model: scribe_v1` | API de transcrição própria da ElevenLabs |
| Endpoint estilo Whisper | `openai/whisper-1`, `groq/whisper-large-v3` | Endpoint `/audio/transcriptions` compatível com OpenAI |
| Chat multimodal (em construção) | `openai/gpt-4o-audio-preview`, `gemini/gemini-2.5-flash` | Manda o áudio para um modelo de chat multimodal e pede a transcrição |

`DetectTranscriber` resolve nesta ordem: (1) `voice.model_name` contra
`model_list` — caminho preferido; (2) se não resolver, varredura de
compatibilidade legada em `model_list` por entradas ASR auto-detectadas.
Configuração nova deve sempre setar `voice.model_name` explicitamente.

Erros comuns: definir o modelo ASR em `model_list` e esquecer
`voice.model_name`; colocar a chave dentro de `voice` em vez de
`.security.yml`; usar `api_base` customizado apontando pro provider errado.

## TTS (texto → fala)

Recomendação rápida: OpenAI (`openai/tts-1`, caminho mais bem suportado —
o runtime hoje é construído em torno do formato de requisição
`/audio/speech` da OpenAI) ou Xiaomi MiMo como segunda opção
OpenAI-compatível.

```json
{
  "voice": { "tts_model_name": "openai-tts" },
  "model_list": [{ "model_name": "openai-tts", "model": "openai/tts-1" }]
}
```

Provider que precisa de parâmetros próprios (voz customizada, formato de
resposta) usa `model_list[].extra_body` — exemplo real, OpenRouter
`microsoft/mai-voice-2`:

```json
{
  "model_name": "mai-voice-2",
  "provider": "openrouter",
  "model": "microsoft/mai-voice-2",
  "api_base": "https://openrouter.ai/api/v1",
  "extra_body": { "voice": "en-US-Harper:MAI-Voice-2", "response_format": "mp3" }
}
```

Defaults do runtime hoje: endpoint `/audio/speech`, formato `opus`, voz
`alloy` — todos sobrescrevíveis por `extra_body`. Se o provider rejeitar
`response_format`, o Kuromatsu tenta de novo uma vez sem esse campo.

`DetectTTS` resolve: (1) `voice.tts_model_name` contra `model_list`,
caminho preferido; (2) se não setado/não resolvido, varredura por uma
entrada cujo `model` contém `tts` e tem chave configurada (compatibilidade,
configuração nova deve setar o nome explicitamente).

Normalização de `api_base`: para OpenAI, uma base como
`https://api.openai.com` ou `.../v1` se torna
`https://api.openai.com/v1/audio/speech`; para outros providers
OpenAI-compatíveis, o Kuromatsu preserva o caminho configurado garantindo
que termine em `/audio/speech`. Sem `api_base`, usa o default do provider
quando o prefixo do modelo é conhecido.

Erros comuns: `voice.tts_model_name` sem entrada correspondente em
`model_list`; esquecer a chave em `.security.yml`; assumir que o Kuromatsu
infere sozinho uma voz customizada sem `extra_body`; usar um endpoint que
não é compatível com o formato `/audio/speech` da OpenAI.

## Checklist mínimo antes de testar

- ASR: `voice.model_name` casa com um `model_list[].model_name`; a chave em
  `.security.yml` existe e é válida; o modelo escolhido é de fato
  ASR-capable; entrada de voz está habilitada no canal em uso.
- TTS: `voice.tts_model_name` casa com um `model_list[].model_name`; chave
  válida em `.security.yml`; o provider suporta um endpoint de síntese
  compatível com OpenAI; o modelo escolhido é de fato TTS-capable.

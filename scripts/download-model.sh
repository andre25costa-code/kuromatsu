#!/usr/bin/env bash
# Baixa o modelo Bonsai GGUF com verificação de integridade (FR-012 / ADR-009).
# Uso: scripts/download-model.sh [destino]
#   BONSAI_MODEL_URL=<url> para sobrescrever a fonte de download.
set -euo pipefail

DEST="${1:-models/Bonsai-1.7B-Q1_0.gguf}"
SHA256_EXPECTED="3d7c6c90dd98717a203adb22d5eacd2581850e40aa5327e144b97766cae5f7e3"

# Fonte oficial confirmada (S25): coleção HF prism-ml/Bonsai-1.7B-gguf.
# X-Linked-ETag do HEAD bate exatamente com SHA256_EXPECTED acima.
BONSAI_MODEL_URL="${BONSAI_MODEL_URL:-https://huggingface.co/prism-ml/Bonsai-1.7B-gguf/resolve/main/Bonsai-1.7B-Q1_0.gguf}"

checksum() { sha256sum "$1" | awk '{print $1}'; }

if [ -f "$DEST" ]; then
    if [ "$(checksum "$DEST")" = "$SHA256_EXPECTED" ]; then
        echo "ok: $DEST ja presente e integro"
        exit 0
    fi
    echo "aviso: $DEST existe mas o SHA256 nao confere; baixando novamente" >&2
fi

mkdir -p "$(dirname "$DEST")"
TMP="$DEST.part"
curl -fL --retry 3 -o "$TMP" "$BONSAI_MODEL_URL"

ACTUAL="$(checksum "$TMP")"
if [ "$ACTUAL" != "$SHA256_EXPECTED" ]; then
    rm -f "$TMP"
    echo "erro: SHA256 divergente ($ACTUAL != $SHA256_EXPECTED); download rejeitado" >&2
    exit 1
fi

mv "$TMP" "$DEST"
echo "ok: modelo baixado em $DEST"

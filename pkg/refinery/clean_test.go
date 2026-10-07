package refinery

import (
	"strings"
	"testing"
)

// AC-024-1: ANSI sequences, control characters and HTML comments go away;
// fenced code blocks survive byte for byte.
func TestClean_RemovesNoiseKeepsCodeBlocks(t *testing.T) {
	code := "```sh\nprintf '\\x1b[31m' <!-- not a comment here -->\n```"
	in := "\x1b[31mErro\x1b[0m no deploy\x07 <!-- nota interna -->ok\n" + code + "\nfim"

	got := Clean(in)
	if strings.Contains(got, "\x1b[") || strings.Contains(got, "\x07") || strings.Contains(got, "nota interna") {
		t.Fatalf("Clean() kept noise: %q", got)
	}
	if !strings.Contains(got, "Erro no deploy ok") {
		t.Fatalf("Clean() damaged the text: %q", got)
	}
	if !strings.Contains(got, code) {
		t.Fatalf("Clean() changed the fenced code block: %q", got)
	}
}

// AC-024-5: user text loses only ANSI/control characters -- never words,
// not even something that looks like an HTML comment.
func TestCleanUserText_OnlyStripsTerminalNoise(t *testing.T) {
	in := "\x1b[1mlembra\x1b[0m de mim <!-- isto é meu -->"
	if got := CleanUserText(in); got != "lembra de mim <!-- isto é meu -->" {
		t.Fatalf("CleanUserText() = %q", got)
	}
}

// AC-024-2: assistant acknowledgement-only turns are dropped from digests.
func TestIsAssistantAck(t *testing.T) {
	for _, ack := range []string{"ok", "Ok!", "entendido.", "  Perfeito  ", "beleza", "feito…"} {
		if !IsAssistantAck(ack) {
			t.Fatalf("IsAssistantAck(%q) = false, want true", ack)
		}
	}
	for _, real := range []string{"ok, vou reiniciar o serviço", "Entendido: o backup roda às 3h", ""} {
		if IsAssistantAck(real) {
			t.Fatalf("IsAssistantAck(%q) = true, want false", real)
		}
	}
}

// AC-024-3 (P4): credentials are detected (for the "sensitive" flag) but
// never removed from what is persisted.
func TestContainsCredential(t *testing.T) {
	if !ContainsCredential("chave sk-proj-abcdefghijklmnop1234 aqui") ||
		!ContainsCredential("password=hunter22xx") {
		t.Fatal("ContainsCredential() missed a credential")
	}
	if ContainsCredential("mandar flores para a Duda em 12/03") {
		t.Fatal("ContainsCredential() flagged personal data that is not a credential")
	}
}

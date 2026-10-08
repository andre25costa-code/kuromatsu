package refinery

import "testing"

const (
	baseFact    = "O usuário prefere receber o relatório de carga às sete da manhã pelo Telegram."
	nearFact    = "O usuário prefere receber o relatório de carga às oito da manhã pelo Telegram."
	unrelatedFt = "A VM de referência roda systemd 252 com cgroup v2 e um limite de memória de 900M."
)

func TestSimHash64_IdenticalAfterNormalization(t *testing.T) {
	a := SimHash64(baseFact)
	b := SimHash64("  o USUÁRIO prefere   receber o relatório de carga às sete da manhã pelo telegram. ")
	if d := Hamming(a, b); d != 0 {
		t.Fatalf("Hamming(case/whitespace variants) = %d, want 0", d)
	}
}

// ADR-020 §2: Hamming between SimHashes approximates the angle between the
// hashed n-gram vectors -- a one-word edit stays close, an unrelated text
// lands near 32 (half the bits).
func TestSimHash64_NearDuplicateCloseUnrelatedFar(t *testing.T) {
	base := SimHash64(baseFact)
	near := Hamming(base, SimHash64(nearFact))
	far := Hamming(base, SimHash64(unrelatedFt))
	if near > 12 {
		t.Fatalf("Hamming(near duplicate) = %d, want <= 12", near)
	}
	if far < 20 {
		t.Fatalf("Hamming(unrelated) = %d, want >= 20", far)
	}
	if near >= far {
		t.Fatalf("near (%d) not closer than unrelated (%d)", near, far)
	}
}

func TestNearest_LinearScanFindsClosest(t *testing.T) {
	index := []uint64{SimHash64(unrelatedFt), SimHash64(baseFact), SimHash64("Outro fato qualquer sobre backups.")}

	i, d := Nearest(index, SimHash64(nearFact))
	if i != 1 {
		t.Fatalf("Nearest() = index %d (distance %d), want 1", i, d)
	}
}

func TestNearest_EmptyIndex(t *testing.T) {
	if i, d := Nearest(nil, SimHash64(baseFact)); i != -1 || d != 65 {
		t.Fatalf("Nearest(empty) = (%d, %d), want (-1, 65)", i, d)
	}
}

// Review finding: "ã" precomposed (NFC, Windows/Linux) and decomposed (NFD,
// pasted from macOS) must fingerprint the same.
func TestSimHash64_UnicodeNormalized(t *testing.T) {
	nfc := "O usuário mora em Recife e não gosta de café."
	nfd := "O usua\u0301rio mora em Recife e na\u0303o gosta de cafe\u0301."
	if d := Hamming(SimHash64(nfc), SimHash64(nfd)); d != 0 {
		t.Fatalf("Hamming(NFC, NFD) = %d, want 0", d)
	}
}

// A repeat that differs only in punctuation is the same text: punctuation
// must not move the fingerprint (letters and digits -- a changed value --
// still do).
func TestSimHash64_PunctuationInsensitive(t *testing.T) {
	a := SimHash64("Nossa recepção perde umas 3 horas por dia confirmando consulta por WhatsApp, um por um.")
	b := SimHash64("nossa recepção perde umas 3 horas por dia confirmando consulta por whatsapp um por um")
	if d := Hamming(a, b); d != 0 {
		t.Fatalf("Hamming(punctuation variants) = %d, want 0", d)
	}
	if Hamming(SimHash64("perde 3 horas por dia"), SimHash64("perde 8 horas por dia")) == 0 {
		t.Fatal("a changed digit no longer moves the fingerprint")
	}
}

// Texts shorter than the 3-rune n-gram produce no n-grams; they must still
// get distinct fingerprints, otherwise every short text collides at 0 and the
// first one absorbs all the others in dedupe.
func TestSimHash64_ShortTextsDoNotCollide(t *testing.T) {
	ok, no := SimHash64("ok"), SimHash64("no")
	if ok == no {
		t.Fatalf("SimHash64(\"ok\") == SimHash64(\"no\") == %#x", ok)
	}
	if ok == 0 || no == 0 {
		t.Fatalf("short text fingerprinted as 0: ok=%#x no=%#x", ok, no)
	}
	if SimHash64("OK!") != ok {
		t.Fatal("normalization (case, punctuation) must still apply to short texts")
	}
	if SimHash64("") != 0 || SimHash64("  ...  ") != 0 {
		t.Fatal("empty text (after normalization) keeps fingerprint 0")
	}
}

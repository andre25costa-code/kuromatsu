// Command nativebench measures the native provider's generation speed
// (tokens/s) and process RSS against a real GGUF file, without going
// through the full agent pipeline. It exists to produce the S39 baseline
// numbers for the deploy target (NFR-002) -- run it on the actual Oracle
// ARM64 box, not on a dev machine, since CPU dot-product/repack kernel
// selection is hardware-dependent (see ADR-003, S18).
//
// Only meaningful on a binary built with `make bench-native` (build tag
// nativellm, CGO_ENABLED=1): the pure-Go stub engine always errors.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/providers/localllm"
	"github.com/andre25costa-code/kuromatsu/pkg/providers/protocoltypes"
)

var fixedPrompts = []string{
	"Em poucas palavras, o que é resiliência?",
	"Liste três tarefas típicas de um assistente pessoal.",
	"Resuma em uma frase o propósito de um bonsai.",
}

func main() {
	modelPath := flag.String("model", "models/Bonsai-1.7B-Q1_0.gguf", "path to the GGUF file")
	nCtx := flag.Int("n-ctx", 2048, "context size")
	maxPredict := flag.Int("max-predict", 128, "max tokens to generate per prompt")
	nThreads := flag.Int("n-threads", 0, "number of decode threads (0 = engine default, min(runtime.NumCPU(),4) -- ADR-015)")
	repeat := flag.Int("repeat", 1, "how many times to repeat the fixed prompt set; >1 shows the KV prefix-cache warming up across repetitions (cached_tokens, B1/ADR-015)")
	flag.Parse()

	if !localllm.Built() {
		fmt.Fprintln(os.Stderr, "nativebench: this binary was built without the nativellm engine; rebuild with `make bench-native`")
		os.Exit(1)
	}

	if *repeat < 1 {
		fmt.Fprintln(os.Stderr, "nativebench: -repeat must be >= 1")
		os.Exit(1)
	}

	if _, err := os.Stat(*modelPath); err != nil {
		fmt.Fprintf(os.Stderr, "nativebench: model not found at %s: %v\n", *modelPath, err)
		os.Exit(1)
	}

	provider, err := localllm.NewProvider(localllm.Options{
		ModelPath:  *modelPath,
		NCtx:       *nCtx,
		MaxPredict: *maxPredict,
		NThreads:   *nThreads,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "nativebench: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("model: %s\n", *modelPath)
	fmt.Printf("n_ctx: %d, max_predict: %d, n_threads: %d, repeat: %d\n\n", *nCtx, *maxPredict, *nThreads, *repeat)
	reportRSS("before load")

	var totalPromptTok, totalOutputTok, totalCachedTok int
	var totalGenTime time.Duration
	firstPrompt := true

	for rep := 0; rep < *repeat; rep++ {
		if *repeat > 1 {
			fmt.Printf("--- repetition %d/%d ---\n", rep+1, *repeat)
		}

		for i, prompt := range fixedPrompts {
			messages := []protocoltypes.Message{
				{Role: "system", Content: "Você é um assistente pessoal objetivo."},
				{Role: "user", Content: prompt},
			}

			start := time.Now()
			resp, err := provider.Chat(context.Background(), messages, nil, provider.GetDefaultModel(), nil)
			elapsed := time.Since(start)
			if err != nil {
				fmt.Fprintf(os.Stderr, "prompt %d (rep %d) failed: %v\n", i+1, rep+1, err)
				continue
			}

			promptTok := resp.Usage.PromptTokens
			outputTok := resp.Usage.CompletionTokens
			cachedTok := resp.Usage.CachedTokens
			totalPromptTok += promptTok
			totalOutputTok += outputTok
			totalCachedTok += cachedTok

			// First run pays the mmap/load cost; report it separately from
			// generation speed to avoid skewing the tok/s figure.
			if firstPrompt {
				reportRSS("after first load")
				firstPrompt = false
			}

			genToksPerSec := 0.0
			if elapsed > 0 {
				genToksPerSec = float64(outputTok) / elapsed.Seconds()
			}
			totalGenTime += elapsed

			fmt.Printf("[%d] prompt_tok=%d cached_tok=%d output_tok=%d elapsed=%s tok/s=%.2f prefill_ms=%d gen_ms=%d\n",
				i+1, promptTok, cachedTok, outputTok, elapsed.Round(time.Millisecond), genToksPerSec,
				resp.Usage.PrefillMs, resp.Usage.GenerationMs)
		}
	}

	fmt.Println()
	if totalGenTime > 0 {
		fmt.Printf("overall: prompt_tok=%d cached_tok=%d output_tok=%d avg_tok/s=%.2f\n",
			totalPromptTok, totalCachedTok, totalOutputTok, float64(totalOutputTok)/totalGenTime.Seconds())
	}
	reportRSS("after all prompts")
}

func reportRSS(label string) {
	rss, err := readRSS()
	if err != nil {
		fmt.Printf("RSS (%s): unavailable (%v)\n", label, err)
		return
	}
	fmt.Printf("RSS (%s): %.1f MiB\n", label, float64(rss)/(1024*1024))
}

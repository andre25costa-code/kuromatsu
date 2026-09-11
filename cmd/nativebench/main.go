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
	flag.Parse()

	if !localllm.Built() {
		fmt.Fprintln(os.Stderr, "nativebench: this binary was built without the nativellm engine; rebuild with `make bench-native`")
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
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "nativebench: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("model: %s\n", *modelPath)
	fmt.Printf("n_ctx: %d, max_predict: %d\n\n", *nCtx, *maxPredict)
	reportRSS("before load")

	var totalPromptTok, totalOutputTok int
	var totalGenTime time.Duration

	for i, prompt := range fixedPrompts {
		messages := []protocoltypes.Message{
			{Role: "system", Content: "Você é um assistente pessoal objetivo."},
			{Role: "user", Content: prompt},
		}

		start := time.Now()
		resp, err := provider.Chat(context.Background(), messages, nil, provider.GetDefaultModel(), nil)
		elapsed := time.Since(start)
		if err != nil {
			fmt.Fprintf(os.Stderr, "prompt %d failed: %v\n", i+1, err)
			continue
		}

		promptTok := resp.Usage.PromptTokens
		outputTok := resp.Usage.CompletionTokens
		totalPromptTok += promptTok
		totalOutputTok += outputTok

		// First run pays the mmap/load cost; report it separately from
		// generation speed to avoid skewing the tok/s figure.
		if i == 0 {
			reportRSS("after first load")
		}

		genToksPerSec := 0.0
		if elapsed > 0 {
			genToksPerSec = float64(outputTok) / elapsed.Seconds()
		}
		totalGenTime += elapsed

		fmt.Printf("[%d] prompt_tok=%d output_tok=%d elapsed=%s tok/s=%.2f\n",
			i+1, promptTok, outputTok, elapsed.Round(time.Millisecond), genToksPerSec)
	}

	fmt.Println()
	if totalGenTime > 0 {
		fmt.Printf("overall: prompt_tok=%d output_tok=%d avg_tok/s=%.2f\n",
			totalPromptTok, totalOutputTok, float64(totalOutputTok)/totalGenTime.Seconds())
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

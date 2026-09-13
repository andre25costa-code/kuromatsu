package localllm

import "testing"

func TestCommonPrefixLen(t *testing.T) {
	cases := []struct {
		name string
		a, b []int
		want int
	}{
		{"both empty", nil, nil, 0},
		{"a empty, b non-empty", nil, []int{1, 2}, 0},
		{"a non-empty, b empty", []int{1, 2}, nil, 0},
		{"identical slices", []int{1, 2, 3}, []int{1, 2, 3}, 3},
		{"no overlap at all", []int{1, 2, 3}, []int{9, 8, 7}, 0},
		{"diverge at first element", []int{5, 2, 3}, []int{1, 2, 3}, 0},
		{"partial prefix match", []int{1, 2, 3, 4}, []int{1, 2, 9, 9}, 2},
		{"a is a strict prefix of b", []int{1, 2}, []int{1, 2, 3, 4}, 2},
		{"b is a strict prefix of a", []int{1, 2, 3, 4}, []int{1, 2}, 2},
		{"single element match", []int{7}, []int{7}, 1},
		{"single element mismatch", []int{7}, []int{8}, 0},
		{"diverge at the very last element", []int{1, 2, 3}, []int{1, 2, 4}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := commonPrefixLen(tc.a, tc.b); got != tc.want {
				t.Fatalf("commonPrefixLen(%v, %v) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
			// commonPrefixLen must be symmetric under argument swap.
			if got := commonPrefixLen(tc.b, tc.a); got != tc.want {
				t.Fatalf("commonPrefixLen(%v, %v) [swapped] = %d, want %d", tc.b, tc.a, got, tc.want)
			}
		})
	}
}

// TestCommonPrefixLen_Generic exercises the generic type parameter with a
// non-int comparable type. The real caller compares []C.llama_token (an
// int32 alias), which can't be exercised without cgo; strings here prove
// the function is genuinely generic rather than accidentally int-only.
func TestCommonPrefixLen_Generic(t *testing.T) {
	got := commonPrefixLen([]string{"a", "b", "c"}, []string{"a", "b", "x"})
	if got != 2 {
		t.Fatalf("commonPrefixLen(strings) = %d, want 2", got)
	}
}

// TestCommonPrefixLen_DoesNotMutateInputs guards against an implementation
// that reslices/writes into a or b -- the real caller passes a live slice
// of the freshly tokenized prompt that must remain untouched.
func TestCommonPrefixLen_DoesNotMutateInputs(t *testing.T) {
	a := []int{1, 2, 3}
	b := []int{1, 2, 3, 4, 5}
	aCopy := append([]int(nil), a...)
	bCopy := append([]int(nil), b...)

	commonPrefixLen(a, b)

	for i := range a {
		if a[i] != aCopy[i] {
			t.Fatalf("a mutated: got %v, want %v", a, aCopy)
		}
	}
	for i := range b {
		if b[i] != bCopy[i] {
			t.Fatalf("b mutated: got %v, want %v", b, bCopy)
		}
	}
}

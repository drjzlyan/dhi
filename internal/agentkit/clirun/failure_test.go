package clirun

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		name    string
		timeout bool
		err     string
		want    Failure
	}{
		{"timeout wins", true, "run interrupted: context deadline exceeded", FailTimeout},
		{"claude not logged in", false, `clirun/claude: run ended with "success": Invalid API key · Please run /login`, FailAuth},
		{"401 in error", false, "API Error: 401 Unauthorized", FailAuth},
		{"codex login stderr", false, "exit 1: Not logged in. Run `codex login`", FailAuth},
		{"429", false, "API Error: 429 Too Many Requests", FailRateLimit},
		{"overloaded", false, "exit 1: overloaded_error: Overloaded", FailRateLimit},
		{"usage limit", false, "Claude AI usage limit reached|1760000000", FailRateLimit},
		{"prompt too long", false, `clirun/claude: run ended with "success": Prompt is too long`, FailOverflow},
		{"context window", false, "exit 1: input exceeds the context window of this model", FailOverflow},
		{"argv budget", false, "assembled prompt exceeds the argv budget (96 KiB) and x has no verified stdin delivery", FailOverflow},
		{"no final", false, "CLI stream ended without a final event", FailNoFinal},
		{"plain crash", false, "exit 2", FailCrash},
		{"watchdog stall", false, "stalled: no output for 15m0s", FailStall},
		{"max turns is a crash", false, "clirun/claude: run ended with \"error_max_turns\"", FailCrash},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Classify(c.timeout, c.err); got != c.want {
				t.Fatalf("Classify = %q, want %q", got, c.want)
			}
		})
	}
}

func TestFailureRetryable(t *testing.T) {
	for f, want := range map[Failure]bool{
		FailAuth: false, FailTimeout: false,
		FailRateLimit: true, FailOverflow: true, FailNoFinal: true, FailCrash: true, FailStall: true,
	} {
		if got := f.Retryable(); got != want {
			t.Fatalf("%s.Retryable() = %v, want %v", f, got, want)
		}
	}
}
